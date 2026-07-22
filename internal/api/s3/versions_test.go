package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3ListObjectVersions(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "verb"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	if _, err := bucket.UpdateVersioning("verb", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}

	for _, body := range [][]byte{[]byte("v1"), []byte("v2"), []byte("v3")} {
		resp := s.do(t, http.MethodPut, "/verb/foo.txt", "", body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put status=%d", resp.StatusCode)
		}
	}

	resp := s.do(t, http.MethodDelete, "/verb/foo.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodPut, "/verb/bar.txt", "", []byte("b1"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put bar status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/verb", "versions=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", resp.StatusCode, body)
	}

	var out ListVersionsResult
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}

	if out.Name != "verb" {
		t.Fatalf("name=%q", out.Name)
	}
	if len(out.Versions) != 4 {
		t.Fatalf("expected 4 versions got %d (%+v)", len(out.Versions), out.Versions)
	}
	if len(out.DeleteMarkers) != 1 {
		t.Fatalf("expected 1 delete marker got %d", len(out.DeleteMarkers))
	}
	if !out.DeleteMarkers[0].IsLatest || out.DeleteMarkers[0].Key != "foo.txt" {
		t.Fatalf("delete marker should be latest of foo.txt, got %+v", out.DeleteMarkers[0])
	}

	var latestCount int
	for _, v := range out.Versions {
		if v.IsLatest {
			latestCount++
		}
	}
	if latestCount != 1 {
		t.Fatalf("expected exactly 1 IsLatest Version (bar.txt), got %d", latestCount)
	}
}

func TestS3ListObjectVersionsPrefix(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "verp"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	if _, err := bucket.UpdateVersioning("verp", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}

	for _, key := range []string{"a/1", "a/2", "b/1"} {
		resp := s.do(t, http.MethodPut, "/verp/"+key, "", []byte("x"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put %s status=%d", key, resp.StatusCode)
		}
	}

	resp := s.do(t, http.MethodGet, "/verp", "versions=&prefix=a/", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListVersionsResult
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Versions) != 2 {
		t.Fatalf("expected 2 versions under prefix a/, got %d", len(out.Versions))
	}
	for _, v := range out.Versions {
		if v.Key[:2] != "a/" {
			t.Fatalf("unexpected key: %s", v.Key)
		}
	}
}

func TestS3ListObjectVersionsPagination(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "verpg"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}
	if _, err := bucket.UpdateVersioning("verpg", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}

	for _, key := range []string{"k1", "k2", "k3"} {
		resp := s.do(t, http.MethodPut, "/verpg/"+key, "", []byte("x"))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("put: %d", resp.StatusCode)
		}
	}

	resp := s.do(t, http.MethodGet, "/verpg", "versions=&max-keys=2", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListVersionsResult
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Versions) != 2 || !out.IsTruncated {
		t.Fatalf("expected truncated 2-version page, got truncated=%v count=%d", out.IsTruncated, len(out.Versions))
	}
	if out.NextKeyMarker == "" {
		t.Fatalf("expected NextKeyMarker")
	}
}
