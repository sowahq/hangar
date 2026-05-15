package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
			fmt.Printf("[FAIL] %-28s %v\n", op, err)
			fails++
			return
		}
		if info == "" {
			fmt.Printf("[ OK ] %s\n", op)
		} else {
			fmt.Printf("[ OK ] %-28s %s\n", op, info)
		}
	}

	lb, err := cli.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err == nil {
		step("ListBuckets", nil, fmt.Sprintf("count=%d", len(lb.Buckets)))
	} else {
		step("ListBuckets", err, "")
	}

	_, err = cli.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket})
	step("HeadBucket", err, "")

	key := "interop/hello.txt"
	body := []byte("hello hangar interop")
	_, err = cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: aws.String("text/plain"),
	})
	step("PutObject", err, "")

	h, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err == nil {
		step("HeadObject", nil, fmt.Sprintf("size=%d ct=%s etag=%s", aws.ToInt64(h.ContentLength), aws.ToString(h.ContentType), aws.ToString(h.ETag)))
	} else {
		step("HeadObject", err, "")
	}

	rawHeadRaw(ctx, endpoint, bucket, key, ak, sk)

	g, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err == nil {
		b, _ := io.ReadAll(g.Body)
		_ = g.Body.Close()
		step("GetObject", nil, fmt.Sprintf("bodyOK=%v len=%d ct=%s", bytes.Equal(b, body), len(b), aws.ToString(g.ContentType)))
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

	copyKey := "interop/hello-copy.txt"
	copySource := bucket + "/" + key
	_, err = cli.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &bucket,
		Key:        &copyKey,
		CopySource: &copySource,
	})
	step("CopyObject", err, "")

	gc, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &copyKey})
	if err == nil {
		b, _ := io.ReadAll(gc.Body)
		_ = gc.Body.Close()
		step("GetObject(copy)", nil, fmt.Sprintf("bodyOK=%v", bytes.Equal(b, body)))
	} else {
		step("GetObject(copy)", err, "")
	}

	mpKey := "interop/mp-large.bin"
	mpData := make([]byte, 12*1024*1024)
	_, _ = rand.Read(mpData)
	uploader := manager.NewUploader(cli, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024
		u.Concurrency = 2
	})
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &bucket,
		Key:    &mpKey,
		Body:   bytes.NewReader(mpData),
	})
	step("Multipart Upload (12MiB)", err, "")

	if err == nil {
		hm, herr := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &mpKey})
		if herr == nil {
			step("HeadObject(mp)", nil, fmt.Sprintf("size=%d etag=%s", aws.ToInt64(hm.ContentLength), aws.ToString(hm.ETag)))
		} else {
			step("HeadObject(mp)", herr, "")
		}
		gm, gerr := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &mpKey})
		if gerr == nil {
			b, _ := io.ReadAll(gm.Body)
			_ = gm.Body.Close()
			h1 := sha256.Sum256(mpData)
			h2 := sha256.Sum256(b)
			step("GetObject(mp)", nil, fmt.Sprintf("len=%d sha256OK=%v", len(b), h1 == h2))
		} else {
			step("GetObject(mp)", gerr, "")
		}
	}

	initKey := "interop/mp-abort.bin"
	cmu, err := cli.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: &initKey})
	step("CreateMultipartUpload", err, "")
	if err == nil {
		lmu, lerr := cli.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: &bucket})
		if lerr == nil {
			step("ListMultipartUploads", nil, fmt.Sprintf("uploads=%d", len(lmu.Uploads)))
		} else {
			step("ListMultipartUploads", lerr, "")
		}
		_, aerr := cli.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   &bucket,
			Key:      &initKey,
			UploadId: cmu.UploadId,
		})
		step("AbortMultipartUpload", aerr, "")
	}

	presign := s3.NewPresignClient(cli)
	pg, err := presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key}, func(o *s3.PresignOptions) {
		o.Expires = 5 * time.Minute
	})
	if err == nil {
		resp, gerr := http.Get(pg.URL)
		if gerr != nil {
			step("Presigned GET", gerr, "")
		} else {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 {
				step("Presigned GET", fmt.Errorf("status=%d body=%q", resp.StatusCode, string(b)), "")
			} else {
				step("Presigned GET", nil, fmt.Sprintf("bodyOK=%v", bytes.Equal(b, body)))
			}
		}
	} else {
		step("Presigned GET", err, "")
	}

	putKey := "interop/presigned-put.txt"
	putBody := []byte("uploaded via presigned PUT")
	pp, err := presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &putKey}, func(o *s3.PresignOptions) {
		o.Expires = 5 * time.Minute
	})
	if err == nil {
		req, _ := http.NewRequest("PUT", pp.URL, bytes.NewReader(putBody))
		req.ContentLength = int64(len(putBody))
		resp, perr := http.DefaultClient.Do(req)
		if perr != nil {
			step("Presigned PUT", perr, "")
		} else {
			rb, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				step("Presigned PUT", fmt.Errorf("status=%d body=%q", resp.StatusCode, string(rb)), "")
			} else {
				step("Presigned PUT", nil, fmt.Sprintf("status=%d", resp.StatusCode))
			}
		}
	} else {
		step("Presigned PUT", err, "")
	}

	delOut, err := cli.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: &bucket,
		Delete: &types.Delete{
			Objects: []types.ObjectIdentifier{
				{Key: aws.String(key)},
				{Key: aws.String(copyKey)},
				{Key: aws.String(mpKey)},
				{Key: aws.String(putKey)},
				{Key: aws.String("interop/does-not-exist.txt")},
			},
			Quiet: aws.Bool(false),
		},
	})
	if err == nil {
		step("DeleteObjects", nil, fmt.Sprintf("deleted=%d errors=%d", len(delOut.Deleted), len(delOut.Errors)))
	} else {
		step("DeleteObjects", err, "")
	}

	_, err = cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		step("HeadObject(after batch delete)", nil, "404 as expected")
	} else {
		step("HeadObject(after batch delete)", fmt.Errorf("object still exists"), "")
	}

	runSSETests(ctx, cli, bucket, &fails, step)

	if fails > 0 {
		fmt.Printf("\n%d FAIL\n", fails)
		os.Exit(1)
	}
	fmt.Println("\nALL OK")
}

func runSSETests(ctx context.Context, cli *s3.Client, bucket string, fails *int, step func(string, error, string)) {
	sseKey := "interop/sse-s3.bin"
	sseBody := []byte("hello SSE-S3 content")

	_, err := cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               &bucket,
		Key:                  &sseKey,
		Body:                 bytes.NewReader(sseBody),
		ServerSideEncryption: types.ServerSideEncryptionAes256,
	})
	step("Put SSE-S3", err, "")

	if err == nil {
		hg, herr := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &sseKey})
		if herr == nil {
			step("Head SSE-S3", nil, fmt.Sprintf("sse=%s", string(hg.ServerSideEncryption)))
		} else {
			step("Head SSE-S3", herr, "")
		}

		gg, gerr := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &sseKey})
		if gerr == nil {
			b, _ := io.ReadAll(gg.Body)
			_ = gg.Body.Close()
			step("Get SSE-S3", nil, fmt.Sprintf("bodyOK=%v sse=%s", bytes.Equal(b, sseBody), string(gg.ServerSideEncryption)))
		} else {
			step("Get SSE-S3", gerr, "")
		}

		_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &sseKey})
	}

	cKey := make([]byte, 32)
	_, _ = rand.Read(cKey)
	cKeyB64 := base64.StdEncoding.EncodeToString(cKey)
	cMD5sum := md5.Sum(cKey)
	cMD5 := base64.StdEncoding.EncodeToString(cMD5sum[:])

	scKey := "interop/sse-c.bin"
	scBody := []byte("hello SSE-C content")

	_, err = cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:               &bucket,
		Key:                  &scKey,
		Body:                 bytes.NewReader(scBody),
		SSECustomerAlgorithm: aws.String("AES256"),
		SSECustomerKey:       aws.String(cKeyB64),
		SSECustomerKeyMD5:    aws.String(cMD5),
	})
	step("Put SSE-C", err, "")

	if err == nil {
		gg, gerr := cli.GetObject(ctx, &s3.GetObjectInput{
			Bucket:               &bucket,
			Key:                  &scKey,
			SSECustomerAlgorithm: aws.String("AES256"),
			SSECustomerKey:       aws.String(cKeyB64),
			SSECustomerKeyMD5:    aws.String(cMD5),
		})
		if gerr == nil {
			b, _ := io.ReadAll(gg.Body)
			_ = gg.Body.Close()
			step("Get SSE-C", nil, fmt.Sprintf("bodyOK=%v", bytes.Equal(b, scBody)))
		} else {
			step("Get SSE-C", gerr, "")
		}

		_, gerrMissing := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &scKey})
		if gerrMissing != nil {
			step("Get SSE-C(missing hdrs)", nil, "rejected as expected")
		} else {
			step("Get SSE-C(missing hdrs)", fmt.Errorf("missing hdrs accepted"), "")
		}

		wrongKey := make([]byte, 32)
		_, _ = rand.Read(wrongKey)
		wrongB64 := base64.StdEncoding.EncodeToString(wrongKey)
		wrongMD5sum := md5.Sum(wrongKey)
		wrongMD5 := base64.StdEncoding.EncodeToString(wrongMD5sum[:])
		_, gerrWrong := cli.GetObject(ctx, &s3.GetObjectInput{
			Bucket:               &bucket,
			Key:                  &scKey,
			SSECustomerAlgorithm: aws.String("AES256"),
			SSECustomerKey:       aws.String(wrongB64),
			SSECustomerKeyMD5:    aws.String(wrongMD5),
		})
		if gerrWrong != nil {
			step("Get SSE-C(wrong key)", nil, "rejected as expected")
		} else {
			step("Get SSE-C(wrong key)", fmt.Errorf("wrong key accepted"), "")
		}

		_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &scKey})
	}
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
