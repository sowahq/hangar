package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3GetObjectAttributesMultiHeader(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "attrmh"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/attrmh/k", "", []byte("hello"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	req := s.sign(t, http.MethodGet, "/attrmh/k", "attributes=", nil)
	req.Header.Add("x-amz-object-attributes", "ETag")
	req.Header.Add("x-amz-object-attributes", "ObjectSize")
	req.Header.Add("x-amz-object-attributes", "StorageClass")
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, body)
	}
	var out GetObjectAttributesOutput
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if out.ETag == "" || out.ObjectSize == nil || *out.ObjectSize != 5 || out.StorageClass == "" {
		t.Fatalf("expected all 3 fields, got %+v size=%v", out, out.ObjectSize)
	}
}

func TestS3GetObjectAttributes(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "attrb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	payload := []byte("attrtest")
	resp := s.do(t, http.MethodPut, "/attrb/k", "", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	tests := []struct {
		name        string
		header      string
		wantETag    bool
		wantSize    bool
		wantStorage bool
	}{
		{"all", "", true, true, true},
		{"etag only", "ETag", true, false, false},
		{"size + storage", "ObjectSize,StorageClass", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := s.sign(t, http.MethodGet, "/attrb/k", "attributes=", nil)
			if tt.header != "" {
				req.Header.Set("x-amz-object-attributes", tt.header)
			}
			r, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", r.StatusCode, body)
			}
			var out GetObjectAttributesOutput
			if err := xml.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode: %v body=%s", err, body)
			}
			if tt.wantETag && out.ETag == "" {
				t.Fatalf("expected ETag")
			}
			if !tt.wantETag && out.ETag != "" {
				t.Fatalf("unexpected ETag %q", out.ETag)
			}
			if tt.wantSize && (out.ObjectSize == nil || *out.ObjectSize != int64(len(payload))) {
				t.Fatalf("size mismatch")
			}
			if !tt.wantSize && out.ObjectSize != nil {
				t.Fatalf("unexpected size")
			}
			if tt.wantStorage && out.StorageClass == "" {
				t.Fatalf("expected StorageClass")
			}
		})
	}
}
