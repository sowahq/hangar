package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3ObjectLockRequiresVersioning(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/olbk", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create bucket: %d", r.StatusCode)
	}

	cfg := ObjectLockConfigurationXML{ObjectLockEnabled: "Enabled"}
	body, _ := xml.Marshal(cfg)
	resp := s.do(t, http.MethodPut, "/olbk", "object-lock=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestS3ObjectLockPutGet(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "olbk"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := bucket.UpdateVersioning("olbk", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}

	cfg := ObjectLockConfigurationXML{
		ObjectLockEnabled: "Enabled",
		Rule: &ObjectLockRuleXML{
			DefaultRetention: &DefaultRetentionXML{Mode: "GOVERNANCE", Days: 7},
		},
	}
	body, _ := xml.Marshal(cfg)
	resp := s.do(t, http.MethodPut, "/olbk", "object-lock=", body)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("put: %d %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/olbk", "object-lock=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var got ObjectLockConfigurationXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if got.ObjectLockEnabled != "Enabled" {
		t.Fatalf("not enabled: %+v", got)
	}
	if got.Rule == nil || got.Rule.DefaultRetention == nil ||
		got.Rule.DefaultRetention.Mode != "GOVERNANCE" || got.Rule.DefaultRetention.Days != 7 {
		t.Fatalf("retention mismatch: %+v", got.Rule)
	}
}

func TestS3ObjectLockGetMissing(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/olempty", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}

	resp := s.do(t, http.MethodGet, "/olempty", "object-lock=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
