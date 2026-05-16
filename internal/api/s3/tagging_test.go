package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3BucketTagging(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "tgb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/tgb", "tagging=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on empty tagging, got %d", resp.StatusCode)
	}

	putBody := []byte(`<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag><Tag><Key>team</Key><Value>platform</Value></Tag></TagSet></Tagging>`)
	resp = s.do(t, http.MethodPut, "/tgb", "tagging=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put tagging: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/tgb", "tagging=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get tagging: %d body=%s", resp.StatusCode, body)
	}
	var got TaggingXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TagSet) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(got.TagSet))
	}

	resp = s.do(t, http.MethodDelete, "/tgb", "tagging=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete tagging: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/tgb", "tagging=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", resp.StatusCode)
	}
}

func TestS3BucketTaggingValidation(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "tgbv"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"empty key", `<Tagging><TagSet><Tag><Key></Key><Value>v</Value></Tag></TagSet></Tagging>`, http.StatusBadRequest},
		{"duplicate", `<Tagging><TagSet><Tag><Key>k</Key><Value>a</Value></Tag><Tag><Key>k</Key><Value>b</Value></Tag></TagSet></Tagging>`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.do(t, http.MethodPut, "/tgbv", "tagging=", []byte(tt.body))
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
