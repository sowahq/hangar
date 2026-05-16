package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3ListObjectsV1(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lsv1"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	for _, k := range []string{"a", "b", "c"} {
		resp := s.do(t, http.MethodPut, "/lsv1/"+k, "", []byte("x"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put %s: %d", k, resp.StatusCode)
		}
	}

	tests := []struct {
		name        string
		query       string
		wantKeys    []string
		wantTrunc   bool
		wantNext    string
	}{
		{"all", "", []string{"a", "b", "c"}, false, ""},
		{"with marker", "marker=a", []string{"b", "c"}, false, ""},
		{"truncated", "max-keys=2", []string{"a", "b"}, true, "b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.do(t, http.MethodGet, "/lsv1", tt.query, nil)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			var out ListBucketResultV1
			if err := xml.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode: %v body=%s", err, body)
			}
			if len(out.Contents) != len(tt.wantKeys) {
				t.Fatalf("expected %d keys got %d", len(tt.wantKeys), len(out.Contents))
			}
			for i, k := range tt.wantKeys {
				if out.Contents[i].Key != k {
					t.Fatalf("[%d] key=%s want %s", i, out.Contents[i].Key, k)
				}
			}
			if out.IsTruncated != tt.wantTrunc {
				t.Fatalf("truncated=%v want %v", out.IsTruncated, tt.wantTrunc)
			}
			if out.NextMarker != tt.wantNext {
				t.Fatalf("next=%q want %q", out.NextMarker, tt.wantNext)
			}
		})
	}
}

func TestS3ListObjectsV1Prefix(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lsv1p"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	for _, k := range []string{"docs/a", "docs/b", "img/c"} {
		resp := s.do(t, http.MethodPut, "/lsv1p/"+k, "", []byte("x"))
		resp.Body.Close()
	}

	resp := s.do(t, http.MethodGet, "/lsv1p", "prefix=docs/", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListBucketResultV1
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Contents) != 2 {
		t.Fatalf("expected 2 docs entries, got %d", len(out.Contents))
	}
}
