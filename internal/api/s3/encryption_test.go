package s3

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3BucketEncryptionPutGetDelete(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/encbk", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create bucket: %d", r.StatusCode)
	}

	cfg := ServerSideEncryptionConfigurationXML{Rules: []SSERuleXML{{
		ApplyServerSideEncryptionByDefault: SSEByDefault{SSEAlgorithm: "AES256"},
	}}}
	body, _ := xml.Marshal(cfg)

	resp := s.do(t, http.MethodPut, "/encbk", "encryption=", body)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("put encryption: %d %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/encbk", "encryption=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get encryption: %d", resp.StatusCode)
	}
	var got ServerSideEncryptionConfigurationXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(got.Rules) != 1 || got.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm != "AES256" {
		t.Fatalf("unexpected encryption: %+v", got)
	}

	resp = s.do(t, http.MethodDelete, "/encbk", "encryption=", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete encryption: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/encbk", "encryption=", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestS3BucketEncryptionRejectsUnsupportedAlgo(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/encbad", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create bucket: %d", r.StatusCode)
	}

	cfg := ServerSideEncryptionConfigurationXML{Rules: []SSERuleXML{{
		ApplyServerSideEncryptionByDefault: SSEByDefault{SSEAlgorithm: "aws:kms"},
	}}}
	body, _ := xml.Marshal(cfg)

	resp := s.do(t, http.MethodPut, "/encbad", "encryption=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=400", resp.StatusCode)
	}
}

func TestS3BucketEncryptionDefaultAppliedOnPut(t *testing.T) {
	s := newS3TestServer(t)
	enableMasterKey(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encdef"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := bucket.PutEncryption("encdef", &bucket.EncryptionConfig{Algorithm: "AES256"}); err != nil {
		t.Fatalf("put encryption: %v", err)
	}

	payload := bytes.Repeat([]byte("D"), 500)
	resp := s.do(t, http.MethodPut, "/encdef/x.txt", "", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodHead, "/encdef/x.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("x-amz-server-side-encryption"); got != "AES256" {
		t.Fatalf("expected SSE applied, got %q", got)
	}

	resp = s.do(t, http.MethodGet, "/encdef/x.txt", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(body, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestS3BucketEncryptionNoOverrideWhenHeaderPresent(t *testing.T) {
	s := newS3TestServer(t)
	enableMasterKey(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encover"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := bucket.PutEncryption("encover", &bucket.EncryptionConfig{Algorithm: "AES256"}); err != nil {
		t.Fatalf("put encryption: %v", err)
	}

	custKey := randKey(t)
	payload := []byte("explicit-sse-c")
	req := s.sign(t, http.MethodPut, "/encover/x.txt", "", payload)
	sseCHeaders(req, custKey)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("x-amz-server-side-encryption-customer-algorithm"); got != "AES256" {
		t.Fatalf("expected SSE-C header echo, got %q", got)
	}
	if got := resp.Header.Get("x-amz-server-side-encryption"); got != "" {
		t.Fatalf("SSE-S3 must not be applied when SSE-C header present, got %q", got)
	}
}

func TestS3BucketEncryptionDefaultAppliedOnMultipart(t *testing.T) {
	s := newS3TestServer(t)
	enableMasterKey(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encmpu"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := bucket.PutEncryption("encmpu", &bucket.EncryptionConfig{Algorithm: "AES256"}); err != nil {
		t.Fatalf("put encryption: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/encmpu/big.bin", "uploads=", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("init status=%d body=%s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("x-amz-server-side-encryption"); got != "AES256" {
		t.Fatalf("expected SSE applied on initiate, got %q", got)
	}
}
