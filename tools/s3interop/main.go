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
	"mime/multipart"
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
	runNewFeatureTests(ctx, cli, bucket, ak, sk, endpoint, &fails, step)
	runEdgeCaseTests(ctx, cli, bucket, &fails, step)

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

func multipartNew(buf *bytes.Buffer) *multipart.Writer {
	return multipart.NewWriter(buf)
}

func runNewFeatureTests(ctx context.Context, cli *s3.Client, bucket, ak, sk, endpoint string, fails *int, step func(string, error, string)) {
	// Bucket versioning XML
	_, err := cli.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  &bucket,
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	step("PutBucketVersioning", err, "")
	gv, err := cli.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &bucket})
	if err == nil {
		step("GetBucketVersioning", nil, fmt.Sprintf("status=%s", string(gv.Status)))
	} else {
		step("GetBucketVersioning", err, "")
	}

	// Conditional headers
	condKey := "interop/cond.bin"
	pr, err := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &condKey, Body: bytes.NewReader([]byte("v1"))})
	if err == nil {
		etag := aws.ToString(pr.ETag)
		_, gErr := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &condKey, IfMatch: &etag})
		step("GetObject(If-Match ok)", gErr, "")
		bogus := `"deadbeef"`
		_, gErr = cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &condKey, IfMatch: &bogus})
		if gErr != nil {
			step("GetObject(If-Match fail)", nil, "412 as expected")
		} else {
			step("GetObject(If-Match fail)", fmt.Errorf("accepted bad If-Match"), "")
		}
		star := "*"
		_, pErr := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &condKey, Body: bytes.NewReader([]byte("v2")), IfNoneMatch: &star})
		if pErr != nil {
			step("PutObject(If-None-Match: *)", nil, "412 as expected (object exists)")
		} else {
			step("PutObject(If-None-Match: *)", fmt.Errorf("overwrote existing"), "")
		}
	}

	// Bucket tagging
	_, err = cli.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: &bucket,
		Tagging: &types.Tagging{
			TagSet: []types.Tag{
				{Key: aws.String("env"), Value: aws.String("prod")},
				{Key: aws.String("team"), Value: aws.String("platform")},
			},
		},
	})
	step("PutBucketTagging", err, "")
	bt, err := cli.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: &bucket})
	if err == nil {
		step("GetBucketTagging", nil, fmt.Sprintf("tags=%d", len(bt.TagSet)))
	} else {
		step("GetBucketTagging", err, "")
	}
	_, err = cli.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{Bucket: &bucket})
	step("DeleteBucketTagging", err, "")

	// Object tagging via x-amz-tagging header
	tagKey := "interop/tagged.bin"
	_, err = cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket:  &bucket,
		Key:     &tagKey,
		Body:    bytes.NewReader([]byte("tagged")),
		Tagging: aws.String("a=1&b=2"),
	})
	step("PutObject(x-amz-tagging)", err, "")
	ot, err := cli.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: &bucket, Key: &tagKey})
	if err == nil {
		step("GetObjectTagging", nil, fmt.Sprintf("tags=%d", len(ot.TagSet)))
	} else {
		step("GetObjectTagging", err, "")
	}
	hot, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &tagKey})
	if err == nil {
		step("HeadObject(tagging-count)", nil, fmt.Sprintf("count=%d", aws.ToInt32(hot.TagCount)))
	} else {
		step("HeadObject(tagging-count)", err, "")
	}
	_, err = cli.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
		Bucket: &bucket, Key: &tagKey,
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("only"), Value: aws.String("one")}}},
	})
	step("PutObjectTagging", err, "")
	ot2, err := cli.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: &bucket, Key: &tagKey})
	if err == nil {
		got := "none"
		if len(ot2.TagSet) > 0 {
			got = aws.ToString(ot2.TagSet[0].Key)
		}
		step("GetObjectTagging(after replace)", nil, fmt.Sprintf("tags=%d first=%s", len(ot2.TagSet), got))
	} else {
		step("GetObjectTagging(after replace)", err, "")
	}
	_, err = cli.DeleteObjectTagging(ctx, &s3.DeleteObjectTaggingInput{Bucket: &bucket, Key: &tagKey})
	step("DeleteObjectTagging", err, "")

	// ListObjectVersions
	lov, err := cli.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, Prefix: aws.String("interop/cond")})
	if err == nil {
		step("ListObjectVersions", nil, fmt.Sprintf("versions=%d markers=%d", len(lov.Versions), len(lov.DeleteMarkers)))
	} else {
		step("ListObjectVersions", err, "")
	}

	// ListObjects v1
	lo1, err := cli.ListObjects(ctx, &s3.ListObjectsInput{Bucket: &bucket, Prefix: aws.String("interop/")})
	if err == nil {
		step("ListObjects (v1)", nil, fmt.Sprintf("contents=%d", len(lo1.Contents)))
	} else {
		step("ListObjects (v1)", err, "")
	}

	// GetObjectAttributes
	attr, err := cli.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket:           &bucket,
		Key:              &tagKey,
		ObjectAttributes: []types.ObjectAttributes{types.ObjectAttributesEtag, types.ObjectAttributesObjectSize, types.ObjectAttributesStorageClass},
	})
	if err == nil {
		step("GetObjectAttributes", nil, fmt.Sprintf("size=%d storage=%s", aws.ToInt64(attr.ObjectSize), string(attr.StorageClass)))
	} else {
		step("GetObjectAttributes", err, "")
	}

	// UploadPartCopy
	srcKey := "interop/upc-src.bin"
	srcData := make([]byte, 6*1024*1024)
	_, _ = rand.Read(srcData)
	_, err = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &srcKey, Body: bytes.NewReader(srcData)})
	if err == nil {
		dstKey := "interop/upc-dst.bin"
		cmu, cerr := cli.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: &bucket, Key: &dstKey})
		if cerr == nil {
			copySrc := bucket + "/" + srcKey
			rng := "bytes=0-1048575"
			_, upErr := cli.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
				Bucket:          &bucket,
				Key:             &dstKey,
				PartNumber:      aws.Int32(1),
				UploadId:        cmu.UploadId,
				CopySource:      &copySrc,
				CopySourceRange: &rng,
			})
			step("UploadPartCopy(ranged)", upErr, "")
			_, _ = cli.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: &bucket, Key: &dstKey, UploadId: cmu.UploadId})
		} else {
			step("UploadPartCopy(init)", cerr, "")
		}
		_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &srcKey})
	}

	// POST policy via SDK presigner
	postKey := "interop/post-${filename}"
	pc := s3.NewPresignClient(cli)
	pp, err := pc.PresignPostObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &postKey})
	if err == nil {
		var buf bytes.Buffer
		mw := multipartNew(&buf)
		for k, v := range pp.Values {
			_ = mw.WriteField(k, v)
		}
		fw, _ := mw.CreateFormFile("file", "post-upload.bin")
		_, _ = fw.Write([]byte("via POST"))
		mw.Close()
		req, _ := http.NewRequest("POST", pp.URL, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, perr := http.DefaultClient.Do(req)
		if perr != nil {
			step("POST policy (SDK presigner)", perr, "")
		} else {
			rb, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				step("POST policy (SDK presigner)", fmt.Errorf("status=%d body=%s", resp.StatusCode, rb), "")
			} else {
				step("POST policy (SDK presigner)", nil, fmt.Sprintf("status=%d", resp.StatusCode))
			}
		}
	} else {
		step("POST policy (SDK presigner)", err, "")
	}

	// GetBucketLocation
	loc, err := cli.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: &bucket})
	if err == nil {
		step("GetBucketLocation", nil, fmt.Sprintf("constraint=%q", string(loc.LocationConstraint)))
	} else {
		step("GetBucketLocation", err, "")
	}

	// Copy with source-if-match precondition
	condSrc := "interop/copy-src.bin"
	cpr, _ := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &condSrc, Body: bytes.NewReader([]byte("cp"))})
	if cpr != nil && cpr.ETag != nil {
		etag := aws.ToString(cpr.ETag)
		copyDst := "interop/copy-dst.bin"
		copySrc := bucket + "/" + condSrc
		_, err = cli.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket: &bucket, Key: &copyDst, CopySource: &copySrc,
			CopySourceIfMatch: &etag,
		})
		step("CopyObject(source-if-match ok)", err, "")

		bogus := `"deadbeef"`
		_, err = cli.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket: &bucket, Key: aws.String("interop/copy-dst-fail.bin"), CopySource: &copySrc,
			CopySourceIfMatch: &bogus,
		})
		if err != nil {
			step("CopyObject(source-if-match fail)", nil, "412 as expected")
		} else {
			step("CopyObject(source-if-match fail)", fmt.Errorf("accepted bad if-match"), "")
		}
		_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &copyDst})
		_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &condSrc})
	}

	// ACL stubs
	_, err = cli.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: &bucket})
	step("GetBucketAcl(stub)", err, "")
	_, err = cli.GetBucketLogging(ctx, &s3.GetBucketLoggingInput{Bucket: &bucket})
	step("GetBucketLogging(stub)", err, "")
	_, err = cli.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{Bucket: &bucket})
	step("GetBucketNotification(stub)", err, "")
	_, err = cli.GetBucketRequestPayment(ctx, &s3.GetBucketRequestPaymentInput{Bucket: &bucket})
	step("GetBucketRequestPayment(stub)", err, "")
	_, err = cli.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: &bucket})
	if err != nil {
		step("GetBucketPolicy(404 stub)", nil, "404 as expected")
	} else {
		step("GetBucketPolicy(404 stub)", fmt.Errorf("expected 404"), "")
	}

	// cleanup
	_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &condKey})
	_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &tagKey})
}

func runEdgeCaseTests(ctx context.Context, cli *s3.Client, bucket string, fails *int, step func(string, error, string)) {
	// Range tail (bytes=-N)
	tailKey := "edge/tail.bin"
	tailBody := []byte("abcdefghijklmnop")
	_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &tailKey, Body: bytes.NewReader(tailBody)})
	rng := "bytes=-4"
	gr, err := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &tailKey, Range: &rng})
	if err == nil {
		b, _ := io.ReadAll(gr.Body)
		_ = gr.Body.Close()
		if string(b) == "mnop" {
			step("Range(tail bytes=-4)", nil, fmt.Sprintf("got=%q", b))
		} else {
			step("Range(tail bytes=-4)", fmt.Errorf("got %q want mnop", b), "")
		}
	} else {
		step("Range(tail bytes=-4)", err, "")
	}

	// Range out-of-bounds → 416
	rngBad := "bytes=9999-99999"
	_, err = cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &tailKey, Range: &rngBad})
	if err != nil {
		step("Range(out-of-bounds)", nil, "416 as expected")
	} else {
		step("Range(out-of-bounds)", fmt.Errorf("accepted oob range"), "")
	}

	// ListObjects v1 with delimiter
	for _, k := range []string{"docs/a", "docs/b", "img/c"} {
		_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: aws.String("edge/" + k), Body: bytes.NewReader([]byte("x"))})
	}
	lod, err := cli.ListObjects(ctx, &s3.ListObjectsInput{Bucket: &bucket, Prefix: aws.String("edge/"), Delimiter: aws.String("/")})
	if err == nil {
		step("ListObjects v1(delimiter)", nil, fmt.Sprintf("contents=%d commonPrefixes=%d", len(lod.Contents), len(lod.CommonPrefixes)))
	} else {
		step("ListObjects v1(delimiter)", err, "")
	}

	// Delete versioned object → delete-marker, ListObjectVersions reflects it
	vKey := "edge/versioned.bin"
	_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &vKey, Body: bytes.NewReader([]byte("v1"))})
	_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &vKey, Body: bytes.NewReader([]byte("v2"))})
	_, _ = cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &vKey})
	lv, err := cli.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, Prefix: &vKey})
	if err == nil {
		if len(lv.Versions) == 2 && len(lv.DeleteMarkers) == 1 && aws.ToBool(lv.DeleteMarkers[0].IsLatest) {
			step("Delete-marker after Delete", nil, fmt.Sprintf("v=%d dm=%d", len(lv.Versions), len(lv.DeleteMarkers)))
		} else {
			step("Delete-marker after Delete", fmt.Errorf("v=%d dm=%d latest=%v", len(lv.Versions), len(lv.DeleteMarkers), len(lv.DeleteMarkers) > 0 && aws.ToBool(lv.DeleteMarkers[0].IsLatest)), "")
		}
	} else {
		step("Delete-marker after Delete", err, "")
	}

	// GetObject on deleted versioned object → 404 + x-amz-delete-marker
	_, err = cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &vKey})
	if err != nil {
		step("Get(deleted versioned)", nil, "404 as expected")
	} else {
		step("Get(deleted versioned)", fmt.Errorf("accepted"), "")
	}

	// Restore previous version via versionId
	var prevVID string
	for _, v := range lv.Versions {
		if !aws.ToBool(v.IsLatest) {
			prevVID = aws.ToString(v.VersionId)
			break
		}
	}
	if prevVID == "" && len(lv.Versions) > 0 {
		prevVID = aws.ToString(lv.Versions[0].VersionId)
	}
	if prevVID != "" {
		gv, gerr := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &vKey, VersionId: &prevVID})
		if gerr == nil {
			b, _ := io.ReadAll(gv.Body)
			_ = gv.Body.Close()
			step("Get(versionId)", nil, fmt.Sprintf("body=%q", b))
		} else {
			step("Get(versionId)", gerr, "")
		}
	}

	// Conditional GET If-Modified-Since past → 200
	ifKey := "edge/cond.bin"
	_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &ifKey, Body: bytes.NewReader([]byte("body"))})
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &ifKey, IfModifiedSince: &past})
	step("If-Modified-Since past → 200", err, "")
	future := time.Now().Add(24 * time.Hour)
	_, err = cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &ifKey, IfModifiedSince: &future})
	if err != nil {
		step("If-Modified-Since future → 304", nil, "304 as expected")
	} else {
		step("If-Modified-Since future → 304", fmt.Errorf("accepted"), "")
	}

	// Concurrent writes — 8 PUTs same key, last write wins
	concKey := "edge/concurrent.bin"
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		i := i
		go func() {
			payload := []byte(fmt.Sprintf("writer-%d", i))
			_, e := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &concKey, Body: bytes.NewReader(payload)})
			done <- e
		}()
	}
	concFails := 0
	for i := 0; i < 8; i++ {
		if e := <-done; e != nil {
			concFails++
		}
	}
	if concFails == 0 {
		step("Concurrent PUTs x8", nil, "all succeeded")
	} else {
		step("Concurrent PUTs x8", fmt.Errorf("%d errors", concFails), "")
	}
	hr, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &concKey})
	if err == nil {
		step("HeadObject(after concurrent)", nil, fmt.Sprintf("size=%d", aws.ToInt64(hr.ContentLength)))
	} else {
		step("HeadObject(after concurrent)", err, "")
	}

	// Empty body PUT/GET
	emptyKey := "edge/empty.bin"
	_, err = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &emptyKey, Body: bytes.NewReader(nil)})
	step("PutObject(empty)", err, "")
	hg, err := cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &emptyKey})
	if err == nil && aws.ToInt64(hg.ContentLength) == 0 {
		step("HeadObject(empty)", nil, "size=0")
	} else {
		step("HeadObject(empty)", fmt.Errorf("size mismatch err=%v size=%d", err, aws.ToInt64(hg.ContentLength)), "")
	}

	// Missing key → 404
	_, err = cli.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: aws.String("edge/does-not-exist")})
	if err != nil {
		step("HeadObject(missing)", nil, "404 as expected")
	} else {
		step("HeadObject(missing)", fmt.Errorf("accepted"), "")
	}

	// Special chars in key: space, unicode, deep slashes
	specialKeys := []string{
		"edge/with space.txt",
		"edge/déjà-vu/café.txt",
		"edge/a/b/c/d/e/f/g/leaf.bin",
		"edge/percent%20encoded.txt",
	}
	for _, sk := range specialKeys {
		_, e := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: aws.String(sk), Body: bytes.NewReader([]byte("v"))})
		if e != nil {
			step("PutObject("+sk+")", e, "")
			continue
		}
		g, e := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: aws.String(sk)})
		if e != nil {
			step("GetObject("+sk+")", e, "")
			continue
		}
		b, _ := io.ReadAll(g.Body)
		_ = g.Body.Close()
		if string(b) != "v" {
			step("GetObject("+sk+")", fmt.Errorf("body mismatch"), "")
		} else {
			step("Round-trip("+sk+")", nil, "")
		}
	}

	// Pagination: 1200 keys, V2 with max-keys=100
	for i := 0; i < 1200; i++ {
		_, _ = cli.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: aws.String(fmt.Sprintf("page/k-%04d", i)), Body: bytes.NewReader([]byte("x"))})
	}
	var token *string
	total := 0
	pages := 0
	for {
		lo, perr := cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &bucket, Prefix: aws.String("page/"),
			MaxKeys: aws.Int32(100), ContinuationToken: token,
		})
		if perr != nil {
			step("ListObjectsV2(paginate)", perr, "")
			break
		}
		total += len(lo.Contents)
		pages++
		if !aws.ToBool(lo.IsTruncated) {
			break
		}
		token = lo.NextContinuationToken
		if pages > 50 {
			step("ListObjectsV2(paginate)", fmt.Errorf("too many pages"), "")
			break
		}
	}
	if total == 1200 {
		step("ListObjectsV2(paginate 1200)", nil, fmt.Sprintf("pages=%d total=%d", pages, total))
	} else {
		step("ListObjectsV2(paginate 1200)", fmt.Errorf("got %d total in %d pages", total, pages), "")
	}

	// DeleteObjects batch with mix of present + missing
	dl, err := cli.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: &bucket,
		Delete: &types.Delete{
			Objects: []types.ObjectIdentifier{
				{Key: aws.String("page/k-0000")},
				{Key: aws.String("page/k-0001")},
				{Key: aws.String("page/missing")},
			},
			Quiet: aws.Bool(false),
		},
	})
	if err == nil {
		step("DeleteObjects(mixed)", nil, fmt.Sprintf("deleted=%d errors=%d", len(dl.Deleted), len(dl.Errors)))
	} else {
		step("DeleteObjects(mixed)", err, "")
	}

	// Big multipart 50MiB
	bigKey := "edge/big.bin"
	bigBody := make([]byte, 50*1024*1024)
	_, _ = rand.Read(bigBody)
	uploader := manager.NewUploader(cli, func(u *manager.Uploader) {
		u.PartSize = 8 * 1024 * 1024
		u.Concurrency = 4
	})
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &bigKey, Body: bytes.NewReader(bigBody)})
	step("Multipart 50MiB", err, "")
	if err == nil {
		gg, gerr := cli.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &bigKey})
		if gerr == nil {
			b, _ := io.ReadAll(gg.Body)
			_ = gg.Body.Close()
			h1 := sha256.Sum256(bigBody)
			h2 := sha256.Sum256(b)
			step("Get(50MiB) sha256", nil, fmt.Sprintf("len=%d ok=%v", len(b), h1 == h2))
		} else {
			step("Get(50MiB)", gerr, "")
		}
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
