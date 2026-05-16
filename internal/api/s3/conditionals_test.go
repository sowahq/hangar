package s3

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3ConditionalGet(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "cgb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/cgb/k", "", []byte("hello"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("etag empty")
	}

	tests := []struct {
		name       string
		headerKey  string
		headerVal  string
		wantStatus int
	}{
		{"if-match ok", "If-Match", etag, http.StatusOK},
		{"if-match fail", "If-Match", `"deadbeef"`, http.StatusPreconditionFailed},
		{"if-none-match same etag", "If-None-Match", etag, http.StatusNotModified},
		{"if-unmod-since future", "If-Unmodified-Since", time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat), http.StatusOK},
		{"if-unmod-since past", "If-Unmodified-Since", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat), http.StatusPreconditionFailed},
		{"if-mod-since past", "If-Modified-Since", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat), http.StatusOK},
		{"if-mod-since future", "If-Modified-Since", time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat), http.StatusNotModified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := s.sign(t, http.MethodGet, "/cgb/k", "", nil)
			req.Header.Set(tt.headerKey, tt.headerVal)
			resp, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestS3ConditionalPut(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "cpb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/cpb/k", "", []byte("v1"))
	resp.Body.Close()
	etag := resp.Header.Get("ETag")

	tests := []struct {
		name       string
		path       string
		headerKey  string
		headerVal  string
		wantStatus int
	}{
		{"if-match exists ok", "/cpb/k", "If-Match", etag, http.StatusOK},
		{"if-match wrong etag", "/cpb/k", "If-Match", `"deadbeef"`, http.StatusPreconditionFailed},
		{"if-match missing key", "/cpb/missing", "If-Match", etag, http.StatusPreconditionFailed},
		{"if-none-match * new key", "/cpb/new", "If-None-Match", "*", http.StatusOK},
		{"if-none-match * existing", "/cpb/k", "If-None-Match", "*", http.StatusPreconditionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := s.sign(t, http.MethodPut, tt.path, "", []byte("body"))
			req.Header.Set(tt.headerKey, tt.headerVal)
			resp, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
