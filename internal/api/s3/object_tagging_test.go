package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3ObjectTagging(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "otb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/otb/k", "", []byte("x"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put obj: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/otb/k", "tagging=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial get: %d body=%s", resp.StatusCode, body)
	}
	var got TaggingXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TagSet) != 0 {
		t.Fatalf("expected empty TagSet, got %d", len(got.TagSet))
	}

	putBody := []byte(`<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><TagSet><Tag><Key>owner</Key><Value>mathis</Value></Tag></TagSet></Tagging>`)
	resp = s.do(t, http.MethodPut, "/otb/k", "tagging=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put tagging: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/otb/k", "tagging=", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode after put: %v", err)
	}
	if len(got.TagSet) != 1 || got.TagSet[0].Key != "owner" || got.TagSet[0].Value != "mathis" {
		t.Fatalf("tags mismatch: %+v", got.TagSet)
	}

	resp = s.do(t, http.MethodDelete, "/otb/k", "tagging=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete tagging: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/otb/k", "tagging=", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var after TaggingXML
	if err := xml.Unmarshal(body, &after); err != nil {
		t.Fatalf("decode after delete: %v", err)
	}
	if len(after.TagSet) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(after.TagSet))
	}

	resp = s.do(t, http.MethodPut, "/otb/missing", "tagging=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on missing object, got %d", resp.StatusCode)
	}
}
