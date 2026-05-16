package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3BucketVersioningXML(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "vxb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/vxb", "versioning=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get initial: %d", resp.StatusCode)
	}
	var got VersioningConfigurationXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "" {
		t.Fatalf("expected empty status before enabling, got %q", got.Status)
	}

	putBody := []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>`)
	resp = s.do(t, http.MethodPut, "/vxb", "versioning=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put enable: %d", resp.StatusCode)
	}

	info, err := bucket.GetBucket("vxb")
	if err != nil || !info.VersioningEnabled {
		t.Fatalf("expected versioning enabled in storage, info=%+v err=%v", info, err)
	}

	resp = s.do(t, http.MethodGet, "/vxb", "versioning=", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode after enable: %v", err)
	}
	if got.Status != "Enabled" {
		t.Fatalf("status=%q want Enabled", got.Status)
	}

	putBody = []byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Suspended</Status></VersioningConfiguration>`)
	resp = s.do(t, http.MethodPut, "/vxb", "versioning=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put suspend: %d", resp.StatusCode)
	}
	info, _ = bucket.GetBucket("vxb")
	if info.VersioningEnabled {
		t.Fatalf("expected versioning disabled after Suspended")
	}

	putBody = []byte(`<VersioningConfiguration><Status>Bogus</Status></VersioningConfiguration>`)
	resp = s.do(t, http.MethodPut, "/vxb", "versioning=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus status, got %d", resp.StatusCode)
	}
}
