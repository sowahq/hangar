package s3

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/testutil"
	"github.com/valyala/fasthttp"
)

func TestS3VirtualHostRouting(t *testing.T) {
	testutil.SetupServer(t)
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	app := NewRouter(func() time.Time { return now })

	base := "test.local"
	handler := virtualHostWrap(base, app.Handler())
	server := &fasthttp.Server{Handler: handler, StreamRequestBody: true, DisablePreParseMultipartForm: true}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go server.Serve(ln)
	t.Cleanup(func() { _ = server.Shutdown(); _ = ln.Close() })

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "vhbucket"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	k, err := auth.CreateS3Key([]string{auth.PermAdmin}, nil)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	host := "vhbucket." + base
	addr := ln.Addr().String()
	body := []byte("vh body")
	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])

	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/k", bytes.NewReader(body))
	req.Header.Set("Host", host)
	req.Host = host
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHashHex)

	sigReq := &Request{
		Method:   http.MethodPut,
		Path:     "/k",
		RawQuery: "",
		Headers:  http.Header{},
	}
	sigReq.Headers.Set("Host", host)
	sigReq.Headers.Set("X-Amz-Date", amzDate)
	sigReq.Headers.Set("X-Amz-Content-Sha256", payloadHashHex)
	cr, _, err := CanonicalRequest(sigReq, []string{"host", "x-amz-content-sha256", "x-amz-date"}, payloadHashHex)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	sts := StringToSign(amzDate, date, "us-east-1", "s3", sha256Hex(cr))
	signingKey := DeriveSigningKey(k.SecretKey, date, "us-east-1", "s3")
	sig := Sign(sts, signingKey)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+k.AccessKeyID+"/"+date+"/us-east-1/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature="+sig)

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status=%d", resp.StatusCode)
	}

	// GET via vhost
	greq, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/k", nil)
	greq.Header.Set("Host", host)
	greq.Host = host
	emptyHash := sha256.Sum256(nil)
	emptyHex := hex.EncodeToString(emptyHash[:])
	greq.Header.Set("X-Amz-Date", amzDate)
	greq.Header.Set("X-Amz-Content-Sha256", emptyHex)

	gSigReq := &Request{Method: http.MethodGet, Path: "/k", Headers: http.Header{}}
	gSigReq.Headers.Set("Host", host)
	gSigReq.Headers.Set("X-Amz-Date", amzDate)
	gSigReq.Headers.Set("X-Amz-Content-Sha256", emptyHex)
	gcr, _, _ := CanonicalRequest(gSigReq, []string{"host", "x-amz-content-sha256", "x-amz-date"}, emptyHex)
	gsts := StringToSign(amzDate, date, "us-east-1", "s3", sha256Hex(gcr))
	gsig := Sign(gsts, signingKey)
	greq.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+k.AccessKeyID+"/"+date+"/us-east-1/s3/aws4_request,SignedHeaders=host;x-amz-content-sha256;x-amz-date,Signature="+gsig)

	gresp, err := (&http.Client{Timeout: 5 * time.Second}).Do(greq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	gb, _ := io.ReadAll(gresp.Body)
	gresp.Body.Close()
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", gresp.StatusCode, gb)
	}
	if !bytes.Equal(gb, body) {
		t.Fatalf("body mismatch: %q", gb)
	}
	_ = xml.Name{}
}

func TestVirtualHostBucket(t *testing.T) {
	cases := []struct {
		host, base, want string
	}{
		{"mybucket.localhost", "localhost", "mybucket"},
		{"mybucket.localhost:9000", "localhost", "mybucket"},
		{"mybucket.s3.example.com", "s3.example.com", "mybucket"},
		{"mybucket.s3.example.com:8443", "s3.example.com", "mybucket"},
		{"localhost", "localhost", ""},
		{"foo.bar.baz", "localhost", ""},
		{"sub.deep.localhost", "localhost", ""},
		{"", "localhost", ""},
		{"mybucket.localhost", "", ""},
		{".localhost", "localhost", ""},
	}
	for _, tc := range cases {
		got := virtualHostBucket(tc.host, tc.base)
		if got != tc.want {
			t.Errorf("virtualHostBucket(%q, %q) = %q, want %q", tc.host, tc.base, got, tc.want)
		}
	}
}
