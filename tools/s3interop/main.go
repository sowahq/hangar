package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	endpoint := envOr("S3_ENDPOINT", "http://localhost:9000")
	bucket := envOr("S3_BUCKET", "interop-bucket")
	ak := os.Getenv("S3_AK")
	sk := os.Getenv("S3_SK")
	if ak == "" || sk == "" {
		fmt.Fprintln(os.Stderr, "set S3_AK and S3_SK")
		os.Exit(2)
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cli := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	ctx := context.Background()
	fails := 0

	step := func(op string, err error, info string) {
		if err != nil {
			fmt.Printf("[FAIL] %-20s %v\n", op, err)
			fails++
			return
		}
		if info == "" {
			fmt.Printf("[ OK ] %s\n", op)
		} else {
			fmt.Printf("[ OK ] %-20s %s\n", op, info)
		}
	}

	lb, err := cli.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err == nil {
		step("ListBuckets", nil, fmt.Sprintf("count=%d", len(lb.Buckets)))
	} else {
		step("ListBuckets", err, "")
	}

	key := "interop/hello.txt"
	body := []byte("hello hangar interop")
	_, err = cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   bytes.NewReader(body),
	})
	step("PutObject", err, "")

	h, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err == nil {
		step("HeadObject", nil, fmt.Sprintf("size=%d etag=%s", aws.ToInt64(h.ContentLength), aws.ToString(h.ETag)))
	} else {
		step("HeadObject", err, "")
	}

	rawHeadRaw(ctx, endpoint, bucket, key, ak, sk)

	g, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err == nil {
		b, _ := io.ReadAll(g.Body)
		_ = g.Body.Close()
		step("GetObject", nil, fmt.Sprintf("bodyOK=%v len=%d", bytes.Equal(b, body), len(b)))
	} else {
		step("GetObject", err, "")
	}

	rng := "bytes=0-4"
	gr, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key, Range: &rng})
	if err == nil {
		b, _ := io.ReadAll(gr.Body)
		_ = gr.Body.Close()
		step("GetObject(Range)", nil, fmt.Sprintf("got=%q", string(b)))
	} else {
		step("GetObject(Range)", err, "")
	}

	lo, err := cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: &bucket, Prefix: aws.String("interop/")})
	if err == nil {
		step("ListObjectsV2", nil, fmt.Sprintf("keyCount=%d", aws.ToInt32(lo.KeyCount)))
	} else {
		step("ListObjectsV2", err, "")
	}

	_, err = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &key})
	step("DeleteObject", err, "")

	_, err = cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		step("HeadObject(after delete)", nil, "404 as expected")
	} else {
		step("HeadObject(after delete)", fmt.Errorf("object still exists"), "")
	}

	if fails > 0 {
		fmt.Printf("\n%d FAIL\n", fails)
		os.Exit(1)
	}
	fmt.Println("\nALL OK")
}

func rawHeadRaw(ctx context.Context, endpoint, bucket, key, ak, sk string) {
	url := endpoint + "/" + bucket + "/" + key
	req, _ := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	emptyHash := sha256.Sum256(nil)
	req.Header.Set("x-amz-content-sha256", hex.EncodeToString(emptyHash[:]))
	signer := v4.NewSigner()
	if err := signer.SignHTTP(ctx, aws.Credentials{AccessKeyID: ak, SecretAccessKey: sk}, req, hex.EncodeToString(emptyHash[:]), "s3", "us-east-1", time.Now()); err != nil {
		fmt.Println("sign err:", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("raw HEAD err:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("       raw HEAD status=%d\n", resp.StatusCode)
	for k, v := range resp.Header {
		fmt.Printf("       raw HEAD hdr %s: %v\n", k, v)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
