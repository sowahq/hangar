package s3

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3CopyObjectConditionalHeaders(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "copcond"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/copcond/src", "", []byte("v1"))
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("no etag")
	}

	tests := []struct {
		name       string
		header     string
		value      string
		wantStatus int
	}{
		{"if-match ok", "x-amz-copy-source-if-match", etag, http.StatusOK},
		{"if-match bad", "x-amz-copy-source-if-match", `"deadbeef"`, http.StatusPreconditionFailed},
		{"if-none-match same", "x-amz-copy-source-if-none-match", etag, http.StatusPreconditionFailed},
		{"if-unmod-since past", "x-amz-copy-source-if-unmodified-since", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat), http.StatusPreconditionFailed},
		{"if-unmod-since future", "x-amz-copy-source-if-unmodified-since", time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat), http.StatusOK},
		{"if-mod-since past", "x-amz-copy-source-if-modified-since", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat), http.StatusOK},
		{"if-mod-since future", "x-amz-copy-source-if-modified-since", time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat), http.StatusPreconditionFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := s.sign(t, http.MethodPut, "/copcond/dst-"+tt.name, "", nil)
			req.Header.Set("x-amz-copy-source", "/copcond/src")
			req.Header.Set(tt.header, tt.value)
			r, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			if r.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d", r.StatusCode, tt.wantStatus)
			}
		})
	}
}
