package s3

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func buildPostPolicyForm(t *testing.T, accessKey, secret, bucketName, keyName string, fileBody []byte) (*bytes.Buffer, string) {
	t.Helper()
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	region := "us-east-1"

	policy := map[string]interface{}{
		"expiration": now.Add(1 * time.Hour).Format(time.RFC3339),
		"conditions": []interface{}{
			map[string]string{"bucket": bucketName},
			[]interface{}{"starts-with", "$key", ""},
			[]interface{}{"content-length-range", 0, 1048576},
		},
	}
	pj, _ := json.Marshal(policy)
	policyB64 := base64.StdEncoding.EncodeToString(pj)

	signingKey := DeriveSigningKey(secret, date, region, "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(policyB64)))

	credential := accessKey + "/" + date + "/" + region + "/s3/aws4_request"

	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("key", keyName)
	_ = mw.WriteField("policy", policyB64)
	_ = mw.WriteField("x-amz-algorithm", "AWS4-HMAC-SHA256")
	_ = mw.WriteField("x-amz-credential", credential)
	_ = mw.WriteField("x-amz-date", amzDate)
	_ = mw.WriteField("x-amz-signature", sig)
	fw, _ := mw.CreateFormFile("file", "upload.bin")
	fw.Write(fileBody)
	mw.Close()
	return buf, mw.FormDataContentType()
}

func TestS3PostPolicyHappyPath(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "ppb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	body, ct := buildPostPolicyForm(t, s.key.AccessKeyID, s.key.SecretKey, "ppb", "uploads/file.bin", []byte("hello"))

	req, _ := http.NewRequest(http.MethodPost, s.url+"/ppb", body)
	req.Header.Set("Content-Type", ct)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", resp.StatusCode, rb)
	}

	resp = s.do(t, http.MethodGet, "/ppb/uploads/file.bin", "", nil)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != "hello" {
		t.Fatalf("uploaded body mismatch: %q", got)
	}
}

func TestS3PostPolicyBadSignature(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "ppbs"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	body, ct := buildPostPolicyForm(t, s.key.AccessKeyID, "wrong-secret", "ppbs", "k", []byte("x"))

	req, _ := http.NewRequest(http.MethodPost, s.url+"/ppbs", body)
	req.Header.Set("Content-Type", ct)
	resp, _ := s.client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestS3PostPolicyExpired(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "ppbe"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	expired := time.Now().Add(-1 * time.Hour).UTC()
	amzDate := expired.Format("20060102T150405Z")
	date := expired.Format("20060102")

	policy := map[string]interface{}{
		"expiration": expired.Format(time.RFC3339),
		"conditions": []interface{}{
			map[string]string{"bucket": "ppbe"},
		},
	}
	pj, _ := json.Marshal(policy)
	policyB64 := base64.StdEncoding.EncodeToString(pj)
	signingKey := DeriveSigningKey(s.key.SecretKey, date, "us-east-1", "s3")
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(policyB64)))

	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	_ = mw.WriteField("key", "k")
	_ = mw.WriteField("policy", policyB64)
	_ = mw.WriteField("x-amz-algorithm", "AWS4-HMAC-SHA256")
	_ = mw.WriteField("x-amz-credential", s.key.AccessKeyID+"/"+date+"/us-east-1/s3/aws4_request")
	_ = mw.WriteField("x-amz-date", amzDate)
	_ = mw.WriteField("x-amz-signature", sig)
	fw, _ := mw.CreateFormFile("file", "f")
	fw.Write([]byte("x"))
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, s.url+"/ppbe", buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ := s.client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for expired policy, got %d", resp.StatusCode)
	}
}
