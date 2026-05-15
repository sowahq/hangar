package s3

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/testutil"
	"github.com/gofiber/fiber/v2"
)

type s3TestServer struct {
	app    *fiber.App
	client *http.Client
	url    string
	host   string
	now    time.Time
	key    *auth.S3Key
}

func newS3TestServer(t *testing.T) *s3TestServer {
	t.Helper()
	testutil.SetupServer(t)

	now := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	app := NewRouter(func() time.Time { return now })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- app.Listener(ln) }()
	t.Cleanup(func() {
		_ = app.ShutdownWithTimeout(5 * time.Second)
		<-serveErr
	})

	k, err := auth.CreateS3Key([]string{auth.PermAdmin}, nil)
	if err != nil {
		t.Fatalf("create s3 key: %v", err)
	}

	return &s3TestServer{
		app:    app,
		client: &http.Client{Timeout: 10 * time.Second},
		url:    "http://" + ln.Addr().String(),
		host:   ln.Addr().String(),
		now:    now,
		key:    k,
	}
}

func (s *s3TestServer) sign(t *testing.T, method, path, query string, body []byte) *http.Request {
	t.Helper()
	full := s.url + path
	if query != "" {
		full += "?" + query
	}
	req, err := http.NewRequest(method, full, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Host", s.host)
	amzDate := s.now.Format("20060102T150405Z")
	date := s.now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	hash := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(hash[:])
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sigReq := &Request{
		Method:   method,
		Path:     path,
		RawQuery: query,
		Headers:  http.Header{},
	}
	sigReq.Headers.Set("Host", s.host)
	sigReq.Headers.Set("X-Amz-Date", amzDate)
	sigReq.Headers.Set("X-Amz-Content-Sha256", payloadHash)

	cr, _, err := CanonicalRequest(sigReq, signedHeaders, payloadHash)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	region := "us-east-1"
	sts := StringToSign(amzDate, date, region, "s3", sha256Hex(cr))
	signingKey := DeriveSigningKey(s.key.SecretKey, date, region, "s3")
	sig := Sign(sts, signingKey)

	auth := "AWS4-HMAC-SHA256 " +
		"Credential=" + s.key.AccessKeyID + "/" + date + "/" + region + "/s3/aws4_request," +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date," +
		"Signature=" + sig
	req.Header.Set("Authorization", auth)
	return req
}

func (s *s3TestServer) do(t *testing.T, method, path, query string, body []byte) *http.Response {
	t.Helper()
	req := s.sign(t, method, path, query, body)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func TestS3UnauthenticatedRejected(t *testing.T) {
	s := newS3TestServer(t)
	resp, err := s.client.Get(s.url + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 4xx for missing auth, got %d", resp.StatusCode)
	}
}

func TestS3ListBucketsEmpty(t *testing.T) {
	s := newS3TestServer(t)
	resp := s.do(t, http.MethodGet, "/", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var out ListAllMyBucketsResult
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Buckets) != 0 {
		t.Fatalf("expected 0 buckets, got %d", len(out.Buckets))
	}
}

func TestS3CreateAndListBucket(t *testing.T) {
	s := newS3TestServer(t)
	resp := s.do(t, http.MethodPut, "/mybucket", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create bucket status=%d", resp.StatusCode)
	}
	resp = s.do(t, http.MethodGet, "/", "", nil)
	defer resp.Body.Close()
	var out ListAllMyBucketsResult
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Buckets) != 1 || out.Buckets[0].Name != "mybucket" {
		t.Fatalf("got %+v", out.Buckets)
	}
}

func TestS3PutGetObject(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "data"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}

	payload := []byte("hello from sigv4 s3")
	resp := s.do(t, http.MethodPut, "/data/folder/x.txt", "", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}
	if etag := resp.Header.Get("ETag"); !strings.HasPrefix(etag, `"`) {
		t.Fatalf("bad etag: %q", etag)
	}

	resp = s.do(t, http.MethodGet, "/data/folder/x.txt", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", resp.StatusCode, body)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("payload mismatch: got %q", body)
	}
}

func TestS3ListObjectsV2(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lsbucket"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	for _, k := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		resp := s.do(t, http.MethodPut, "/lsbucket/"+k, "", []byte("x"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put %s status=%d", k, resp.StatusCode)
		}
	}
	resp := s.do(t, http.MethodGet, "/lsbucket", "list-type=2", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var out ListBucketResultV2
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.KeyCount != 3 || len(out.Contents) != 3 {
		t.Fatalf("expected 3 keys, got keyCount=%d contents=%d", out.KeyCount, len(out.Contents))
	}
}

func TestS3DeleteObject(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "del"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/del/a.txt", "", []byte("hi"))
	resp.Body.Close()
	resp = s.do(t, http.MethodDelete, "/del/a.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
	resp = s.do(t, http.MethodGet, "/del/a.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status=%d", resp.StatusCode)
	}
}

func TestS3HeadObjectHeaders(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "headbucket"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	payload := []byte("hello from sigv4 s3")
	resp := s.do(t, http.MethodPut, "/headbucket/x.txt", "", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}
	resp = s.do(t, http.MethodHead, "/headbucket/x.txt", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head status=%d", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD body must be empty, got %q", body)
	}
	if got := resp.Header.Get("Content-Length"); got != "19" {
		t.Fatalf("Content-Length=%q want 19", got)
	}
	lm := resp.Header.Get("Last-Modified")
	if _, err := time.Parse(http.TimeFormat, lm); err != nil {
		t.Fatalf("Last-Modified %q not IMF-fixdate: %v", lm, err)
	}
}

func TestS3HeadBucket(t *testing.T) {
	s := newS3TestServer(t)
	resp := s.do(t, http.MethodHead, "/missing", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("head missing status=%d", resp.StatusCode)
	}
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "hbucket"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	resp = s.do(t, http.MethodHead, "/hbucket", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head exists status=%d", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD body must be empty, got %q", body)
	}
}

func TestS3StreamingPayloadRejected(t *testing.T) {
	s := newS3TestServer(t)
	req := s.sign(t, http.MethodPut, "/x", "", nil)
	req.Header.Set("X-Amz-Content-Sha256", PayloadStreaming)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status=%d want=501", resp.StatusCode)
	}
}

func TestS3BadSignatureRejected(t *testing.T) {
	s := newS3TestServer(t)
	req := s.sign(t, http.MethodGet, "/", "", nil)
	auth := req.Header.Get("Authorization")
	req.Header.Set("Authorization", strings.Replace(auth, "Signature=", "Signature=deadbeef", 1)[:len(auth)])
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want=403", resp.StatusCode)
	}
}
