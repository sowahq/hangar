package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func seedLockedBucket(t *testing.T, name string) {
	t.Helper()
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: name}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	if _, err := bucket.UpdateVersioning(name, true); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if err := bucket.PutObjectLockConfig(name, &bucket.ObjectLockConfig{Enabled: true}); err != nil {
		t.Fatalf("enable lock: %v", err)
	}
}

func TestS3PutObjectWithLockHeaders(t *testing.T) {
	s := newS3TestServer(t)
	seedLockedBucket(t, "objlk")

	retainUntil := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	req := s.sign(t, http.MethodPut, "/objlk/x.txt", "", []byte("locked"))
	req.Header.Set(hdrObjectLockMode, "GOVERNANCE")
	req.Header.Set(hdrObjectLockRetainUntilDate, retainUntil)
	req.Header.Set(hdrObjectLockLegalHold, "ON")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodHead, "/objlk/x.txt", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head: %d", resp.StatusCode)
	}
	if got := resp.Header.Get(hdrObjectLockMode); got != "GOVERNANCE" {
		t.Fatalf("mode echo=%q", got)
	}
	if got := resp.Header.Get(hdrObjectLockLegalHold); got != "ON" {
		t.Fatalf("legal-hold echo=%q", got)
	}
	if got := resp.Header.Get(hdrObjectLockRetainUntilDate); got == "" {
		t.Fatalf("missing retain-until echo")
	}
}

func TestS3PutObjectLockHeadersRejectedWhenBucketDisabled(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "nolk"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	retainUntil := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	req := s.sign(t, http.MethodPut, "/nolk/x.txt", "", []byte("x"))
	req.Header.Set(hdrObjectLockMode, "GOVERNANCE")
	req.Header.Set(hdrObjectLockRetainUntilDate, retainUntil)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestS3PutObjectLockMissingRetainDate(t *testing.T) {
	s := newS3TestServer(t)
	seedLockedBucket(t, "objlk2")

	req := s.sign(t, http.MethodPut, "/objlk2/x.txt", "", []byte("x"))
	req.Header.Set(hdrObjectLockMode, "GOVERNANCE")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestS3RetentionEndpointPutGet(t *testing.T) {
	s := newS3TestServer(t)
	seedLockedBucket(t, "rete")

	resp := s.do(t, http.MethodPut, "/rete/x.txt", "", []byte("body"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed put: %d", resp.StatusCode)
	}

	retainUntil := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	body, _ := xml.Marshal(RetentionXML{Mode: "GOVERNANCE", RetainUntilDate: retainUntil})
	resp = s.do(t, http.MethodPut, "/rete/x.txt", "retention=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put retention: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/rete/x.txt", "retention=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get retention: %d", resp.StatusCode)
	}
	var got RetentionXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if got.Mode != "GOVERNANCE" {
		t.Fatalf("mode=%q", got.Mode)
	}
}

func TestS3RetentionRejectsCompliancyDowngrade(t *testing.T) {
	s := newS3TestServer(t)
	seedLockedBucket(t, "comp")

	resp := s.do(t, http.MethodPut, "/comp/x.txt", "", []byte("body"))
	resp.Body.Close()

	retainUntil := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
	body, _ := xml.Marshal(RetentionXML{Mode: "COMPLIANCE", RetainUntilDate: retainUntil})
	resp = s.do(t, http.MethodPut, "/comp/x.txt", "retention=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put compliance: %d", resp.StatusCode)
	}

	earlier := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	body, _ = xml.Marshal(RetentionXML{Mode: "GOVERNANCE", RetainUntilDate: earlier})
	resp = s.do(t, http.MethodPut, "/comp/x.txt", "retention=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("downgrade should be 403, got %d", resp.StatusCode)
	}
}

func TestS3LegalHoldEndpointPutGet(t *testing.T) {
	s := newS3TestServer(t)
	seedLockedBucket(t, "legalh")

	resp := s.do(t, http.MethodPut, "/legalh/x.txt", "", []byte("body"))
	resp.Body.Close()

	body, _ := xml.Marshal(LegalHoldXML{Status: "ON"})
	resp = s.do(t, http.MethodPut, "/legalh/x.txt", "legal-hold=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put legal-hold: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/legalh/x.txt", "legal-hold=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var got LegalHoldXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if got.Status != "ON" {
		t.Fatalf("status=%q", got.Status)
	}

	body, _ = xml.Marshal(LegalHoldXML{Status: "OFF"})
	resp = s.do(t, http.MethodPut, "/legalh/x.txt", "legal-hold=", body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put off: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodHead, "/legalh/x.txt", "", nil)
	resp.Body.Close()
	if got := resp.Header.Get(hdrObjectLockLegalHold); got != "" {
		t.Fatalf("legal-hold OFF should not echo header, got %q", got)
	}
}

func TestS3RetentionDefaultAppliedOnPut(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "defret"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := bucket.UpdateVersioning("defret", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if err := bucket.PutObjectLockConfig("defret", &bucket.ObjectLockConfig{
		Enabled:          true,
		DefaultRetention: &bucket.DefaultRetention{Mode: "GOVERNANCE", Days: 1},
	}); err != nil {
		t.Fatalf("config: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/defret/x.txt", "", []byte("body"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("put status=%d body=%s", resp.StatusCode, raw)
	}

	resp = s.do(t, http.MethodHead, "/defret/x.txt", "", nil)
	resp.Body.Close()
	if got := resp.Header.Get(hdrObjectLockMode); got != "GOVERNANCE" {
		t.Fatalf("default mode not applied, got %q", got)
	}
	if got := resp.Header.Get(hdrObjectLockRetainUntilDate); got == "" {
		t.Fatalf("default retain-until missing")
	}
}
