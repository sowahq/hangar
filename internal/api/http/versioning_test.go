package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func enableVersioningHTTP(t *testing.T, s *testServer, bucket string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"enabled": true})
	resp := s.do(t, http.MethodPut, "/admin/buckets/"+bucket+"/versioning", bytes.NewReader(body), "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("enable versioning: status=%d body=%s", resp.StatusCode, string(b))
	}
}

func TestVersioningHTTPRoundtrip(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "vbucket")
	enableVersioningHTTP(t, s, "vbucket")
	tok := s.createToken(t, "vbucket", []string{"read", "write", "delete"})

	resp := s.doAuth(t, http.MethodPut, "/vbucket/k", tok, bytes.NewReader([]byte("v1")), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT v1 status=%d", resp.StatusCode)
	}
	var out1 struct {
		VersionID string `json:"version_id"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out1); err != nil {
		t.Fatalf("unmarshal v1: %v", err)
	}
	if out1.VersionID == "" {
		t.Fatal("v1 version_id empty")
	}

	resp = s.doAuth(t, http.MethodPut, "/vbucket/k", tok, bytes.NewReader([]byte("v2-data")), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT v2 status=%d", resp.StatusCode)
	}
	var out2 struct {
		VersionID string `json:"version_id"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out2); err != nil {
		t.Fatalf("unmarshal v2: %v", err)
	}

	resp = s.doAuth(t, http.MethodGet, "/vbucket/k?versionId="+out1.VersionID, tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET v1 status=%d", resp.StatusCode)
	}
	if got := readBody(t, resp); !bytes.Equal(got, []byte("v1")) {
		t.Errorf("v1 body: got=%q want=v1", got)
	}

	resp = s.doAuth(t, http.MethodGet, "/vbucket/k", tok, nil, "")
	if got := readBody(t, resp); !bytes.Equal(got, []byte("v2-data")) {
		t.Errorf("current body: got=%q want=v2-data", got)
	}

	resp = s.doAuth(t, http.MethodGet, "/vbucket/k?versions", tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("LIST versions status=%d", resp.StatusCode)
	}
	var listOut struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(readBody(t, resp), &listOut); err != nil {
		t.Fatalf("list versions unmarshal: %v", err)
	}
	if listOut.Count != 2 {
		t.Errorf("versions count=%d want=2", listOut.Count)
	}

	resp = s.doAuth(t, http.MethodDelete, "/vbucket/k", tok, nil, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", resp.StatusCode)
	}
	if resp.Header.Get("X-Delete-Marker") != "true" {
		t.Errorf("missing X-Delete-Marker header")
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodGet, "/vbucket/k", tok, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete-marker status=%d want=404", resp.StatusCode)
	}
	resp.Body.Close()
}
