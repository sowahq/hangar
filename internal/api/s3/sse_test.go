package s3

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
)

func randKey(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func sseCHeaders(req *http.Request, key []byte) {
	sum := md5.Sum(key)
	req.Header.Set("x-amz-server-side-encryption-customer-algorithm", "AES256")
	req.Header.Set("x-amz-server-side-encryption-customer-key", base64.StdEncoding.EncodeToString(key))
	req.Header.Set("x-amz-server-side-encryption-customer-key-md5", base64.StdEncoding.EncodeToString(sum[:]))
}

func enableMasterKey(t *testing.T) {
	t.Helper()
	master := randKey(t)
	config.SetMasterKeyForTest(master)
	t.Cleanup(func() { config.SetMasterKeyForTest(nil) })
}

func TestS3SSES3MasterKeyMissing(t *testing.T) {
	s := newS3TestServer(t)
	config.SetMasterKeyForTest(nil)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "enc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := s.sign(t, http.MethodPut, "/enc/x.txt", "", []byte("payload"))
	req.Header.Set("x-amz-server-side-encryption", "AES256")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=503", resp.StatusCode)
	}
}

func TestS3SSES3Roundtrip(t *testing.T) {
	s := newS3TestServer(t)
	enableMasterKey(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "enc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	payload := bytes.Repeat([]byte("S"), 1024*2+33)
	req := s.sign(t, http.MethodPut, "/enc/x.txt", "", payload)
	req.Header.Set("x-amz-server-side-encryption", "AES256")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("x-amz-server-side-encryption"); got != "AES256" {
		t.Fatalf("echo=%q want AES256", got)
	}

	resp = s.do(t, http.MethodGet, "/enc/x.txt", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", resp.StatusCode)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("payload mismatch")
	}
	if got := resp.Header.Get("x-amz-server-side-encryption"); got != "AES256" {
		t.Fatalf("get echo=%q want AES256", got)
	}
}

func TestS3SSECRoundtrip(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	custKey := randKey(t)
	payload := bytes.Repeat([]byte("C"), 1024*2+11)

	req := s.sign(t, http.MethodPut, "/encc/x.txt", "", payload)
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
		t.Fatalf("echo algo=%q want AES256", got)
	}

	getReq := s.sign(t, http.MethodGet, "/encc/x.txt", "", nil)
	sseCHeaders(getReq, custKey)
	gresp, err := s.client.Do(getReq)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", gresp.StatusCode)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("payload mismatch")
	}
}

func TestS3SSECMissingHeadersOnGet(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	custKey := randKey(t)
	payload := []byte("hi-sse-c")
	req := s.sign(t, http.MethodPut, "/encc/x.txt", "", payload)
	sseCHeaders(req, custKey)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/encc/x.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("get without sse-c headers: status=%d want=400", resp.StatusCode)
	}

	resp = s.do(t, http.MethodHead, "/encc/x.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("head without sse-c headers: status=%d want=400", resp.StatusCode)
	}
}

func TestS3SSECWrongKey(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "encc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	custKey := randKey(t)
	payload := []byte("wrong-key-test")
	req := s.sign(t, http.MethodPut, "/encc/x.txt", "", payload)
	sseCHeaders(req, custKey)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}

	getReq := s.sign(t, http.MethodGet, "/encc/x.txt", "", nil)
	sseCHeaders(getReq, randKey(t))
	gresp, err := s.client.Do(getReq)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	gresp.Body.Close()
	if gresp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong key: status=%d want=400", gresp.StatusCode)
	}
}

func TestS3SSEInvalidAlgorithm(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "enc"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := s.sign(t, http.MethodPut, "/enc/x.txt", "", []byte("data"))
	req.Header.Set("x-amz-server-side-encryption", "RC4")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=400", resp.StatusCode)
	}
}
