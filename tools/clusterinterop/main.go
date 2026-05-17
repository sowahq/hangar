package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func main() {
	ak := os.Getenv("S3_AK")
	sk := os.Getenv("S3_SK")
	bucket := os.Getenv("S3_BUCKET")
	epA := os.Getenv("S3_A")
	epB := os.Getenv("S3_B")

	a := mk(epA, ak, sk)
	b := mk(epB, ak, sk)

	ctx := context.Background()

	payload := []byte("hello distributed cluster, written to A and read from B " + time.Now().Format(time.RFC3339Nano))

	fmt.Println("== PUT via A ==")
	_, err := a.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String("dist/obj1.txt"),
		Body: bytes.NewReader(payload),
	})
	if err != nil {
		log.Fatal("PUT A:", err)
	}
	fmt.Println("ok")

	fmt.Println("== LIST via B ==")
	lst, err := b.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	if err != nil {
		log.Fatal("LIST B:", err)
	}
	for _, o := range lst.Contents {
		fmt.Printf("  %s (%d bytes)\n", aws.ToString(o.Key), aws.ToInt64(o.Size))
	}

	fmt.Println("== GET via B ==")
	got, err := b.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String("dist/obj1.txt")})
	if err != nil {
		log.Fatal("GET B:", err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if !bytes.Equal(body, payload) {
		log.Fatalf("body mismatch: got=%q want=%q", body, payload)
	}
	fmt.Printf("ok %d bytes matched\n", len(body))

	fmt.Println("== HEAD via B ==")
	h, err := b.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("dist/obj1.txt")})
	if err != nil {
		log.Fatal("HEAD B:", err)
	}
	fmt.Printf("ok size=%d etag=%s\n", aws.ToInt64(h.ContentLength), aws.ToString(h.ETag))

	fmt.Println("== DELETE via B ==")
	_, err = b.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String("dist/obj1.txt")})
	if err != nil {
		log.Fatal("DELETE B:", err)
	}
	fmt.Println("ok")

	fmt.Println("== verify HEAD via A returns 404 ==")
	_, err = a.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String("dist/obj1.txt")})
	if err == nil {
		log.Fatal("expected 404 after delete, got nil")
	}
	fmt.Printf("ok (got %v)\n", err)

	fmt.Println("\nALL OK")
}
