package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func mk(endpoint, ak, sk string) *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")),
	)
	if err != nil {
		log.Fatal(err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

type step struct {
	name string
	fn   func() error
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "wal-catchup" {
		runWALCatchup()
		return
	}

	ak := os.Getenv("S3_AK")
	sk := os.Getenv("S3_SK")
	bucket := os.Getenv("S3_BUCKET")
	epA := os.Getenv("S3_A")
	epB := os.Getenv("S3_B")

	if ak == "" || sk == "" || bucket == "" || epA == "" || epB == "" {
		log.Fatal("set S3_AK S3_SK S3_BUCKET S3_A S3_B")
	}

	a := mk(epA, ak, sk)
	b := mk(epB, ak, sk)

	ctx := context.Background()

	steps := []step{
		{"PutA_GetB_small", func() error {
			payload := []byte("hello distributed cluster " + time.Now().Format(time.RFC3339Nano))
			if _, err := a.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("small/obj1.txt"), Body: bytes.NewReader(payload)}); err != nil {
				return fmt.Errorf("put A: %w", err)
			}
			time.Sleep(200 * time.Millisecond)
			got, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("small/obj1.txt")})
			if err != nil {
				return fmt.Errorf("get B: %w", err)
			}
			defer got.Body.Close()
			body, _ := io.ReadAll(got.Body)
			if !bytes.Equal(body, payload) {
				return fmt.Errorf("body mismatch")
			}
			return nil
		}},
		{"PutB_GetA_medium", func() error {
			payload := make([]byte, 256*1024)
			if _, err := rand.Read(payload); err != nil {
				return err
			}
			if _, err := b.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("med/obj.bin"), Body: bytes.NewReader(payload)}); err != nil {
				return fmt.Errorf("put B: %w", err)
			}
			time.Sleep(200 * time.Millisecond)
			got, err := a.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("med/obj.bin")})
			if err != nil {
				return fmt.Errorf("get A: %w", err)
			}
			defer got.Body.Close()
			body, _ := io.ReadAll(got.Body)
			if !bytes.Equal(body, payload) {
				return fmt.Errorf("body mismatch size=%d", len(body))
			}
			return nil
		}},
		{"List_cross_node", func() error {
			lst, err := b.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
			if err != nil {
				return err
			}
			if len(lst.Contents) < 2 {
				return fmt.Errorf("expected >=2 objects, got %d", len(lst.Contents))
			}
			return nil
		}},
		{"Dedup_check", func() error {
			payload := []byte("dedup-payload-aaa-" + time.Now().Format(time.RFC3339Nano))
			if _, err := a.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("dedup/a.bin"), Body: bytes.NewReader(payload)}); err != nil {
				return err
			}
			if _, err := a.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("dedup/b.bin"), Body: bytes.NewReader(payload)}); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			ga, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("dedup/a.bin")})
			if err != nil {
				return err
			}
			defer ga.Body.Close()
			da, _ := io.ReadAll(ga.Body)

			gb, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("dedup/b.bin")})
			if err != nil {
				return err
			}
			defer gb.Body.Close()
			db, _ := io.ReadAll(gb.Body)

			if !bytes.Equal(da, payload) || !bytes.Equal(db, payload) {
				return fmt.Errorf("dedup payload mismatch")
			}
			return nil
		}},
		{"Delete_propagation", func() error {
			payload := []byte("ephemeral")
			if _, err := a.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String("del/x.txt"), Body: bytes.NewReader(payload)}); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			if _, err := b.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("del/x.txt")}); err != nil {
				return err
			}
			time.Sleep(200 * time.Millisecond)
			_, err := a.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("del/x.txt")})
			if err == nil {
				return fmt.Errorf("expected 404 after cross-node delete")
			}
			return nil
		}},
		{"Multipart_cross_node", func() error {
			key := "mpu/big.bin"
			ini, err := a.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			if err != nil {
				return fmt.Errorf("init mpu: %w", err)
			}

			part1 := make([]byte, 5*1024*1024)
			part2 := make([]byte, 1*1024*1024)
			if _, err := rand.Read(part1); err != nil {
				return err
			}
			if _, err := rand.Read(part2); err != nil {
				return err
			}

			p1, err := a.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(bucket), Key: aws.String(key), UploadId: ini.UploadId, PartNumber: aws.Int32(1), Body: bytes.NewReader(part1)})
			if err != nil {
				return fmt.Errorf("upload part1: %w", err)
			}
			p2, err := a.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String(bucket), Key: aws.String(key), UploadId: ini.UploadId, PartNumber: aws.Int32(2), Body: bytes.NewReader(part2)})
			if err != nil {
				return fmt.Errorf("upload part2: %w", err)
			}

			parts := []s3types.CompletedPart{
				{PartNumber: aws.Int32(1), ETag: p1.ETag},
				{PartNumber: aws.Int32(2), ETag: p2.ETag},
			}
			if _, err := a.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
				Bucket: aws.String(bucket), Key: aws.String(key), UploadId: ini.UploadId,
				MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts},
			}); err != nil {
				return fmt.Errorf("complete mpu: %w", err)
			}

			time.Sleep(300 * time.Millisecond)

			got, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			if err != nil {
				return fmt.Errorf("get from B: %w", err)
			}
			defer got.Body.Close()
			body, _ := io.ReadAll(got.Body)
			expected := append(append([]byte(nil), part1...), part2...)
			if !bytes.Equal(body, expected) {
				return fmt.Errorf("mpu body mismatch: got %d bytes", len(body))
			}
			return nil
		}},
	}

	fails := 0
	for _, st := range steps {
		fmt.Printf("== %s ==\n", st.name)
		if err := st.fn(); err != nil {
			fmt.Printf("[FAIL] %s: %v\n", st.name, err)
			fails++
			continue
		}
		fmt.Println("ok")
	}

	if fails > 0 {
		fmt.Printf("\n%d failure(s)\n", fails)
		os.Exit(1)
	}
	fmt.Println("\nALL OK")
}
