package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestAdminBucketEncryptionRoundtrip(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "encadm")

	body, _ := json.Marshal(map[string]any{"algorithm": "AES256"})
	resp := s.do(t, http.MethodPut, "/admin/buckets/encadm/encryption", bytes.NewReader(body), "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("set: %d %s", resp.StatusCode, raw)
	}

	resp = s.do(t, http.MethodGet, "/admin/buckets/encadm/encryption", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var out struct {
		Algorithm string `json:"algorithm"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Algorithm != "AES256" {
		t.Fatalf("algo=%q", out.Algorithm)
	}

	resp = s.do(t, http.MethodDelete, "/admin/buckets/encadm/encryption", nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/admin/buckets/encadm/encryption", nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: %d", resp.StatusCode)
	}
}

func TestAdminBucketEncryptionRejectsNonAES(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "encbad")

	body, _ := json.Marshal(map[string]any{"algorithm": "aws:kms"})
	resp := s.do(t, http.MethodPut, "/admin/buckets/encbad/encryption", bytes.NewReader(body), "application/json")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", resp.StatusCode)
	}
}

func TestAdminObjectLockRequiresVersioning(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "olnov")

	body, _ := json.Marshal(map[string]any{"enabled": true})
	resp := s.do(t, http.MethodPut, "/admin/buckets/olnov/object-lock", bytes.NewReader(body), "application/json")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d want 409", resp.StatusCode)
	}
}

func TestAdminObjectLockRoundtrip(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "oladm")
	enableVersioningHTTP(t, s, "oladm")

	body, _ := json.Marshal(map[string]any{
		"enabled": true,
		"default_retention": map[string]any{
			"mode": "GOVERNANCE",
			"days": 7,
		},
	})
	resp := s.do(t, http.MethodPut, "/admin/buckets/oladm/object-lock", bytes.NewReader(body), "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("set: %d %s", resp.StatusCode, raw)
	}

	resp = s.do(t, http.MethodGet, "/admin/buckets/oladm/object-lock", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var out struct {
		Enabled          bool `json:"enabled"`
		DefaultRetention struct {
			Mode string `json:"mode"`
			Days int    `json:"days"`
		} `json:"default_retention"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Enabled || out.DefaultRetention.Mode != "GOVERNANCE" || out.DefaultRetention.Days != 7 {
		t.Fatalf("unexpected: %+v", out)
	}
}
