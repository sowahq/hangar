package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuotaExceeded(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "qbb")
	tok := s.createToken(t, "qbb", []string{"read", "write"})

	body, _ := json.Marshal(map[string]any{"max_bytes": 10, "max_objects": 2})
	resp := s.do(t, http.MethodPut, "/admin/buckets/qbb/quota", bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("set quota status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodPut, "/qbb/a.txt", tok, bytes.NewReader([]byte("12345")), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("first upload status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodPut, "/qbb/b.txt", tok, bytes.NewReader([]byte("12345678901234")), "application/octet-stream")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 got %d", resp.StatusCode)
	}
}

func TestQuotaLengthRequired(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "qlen")
	tok := s.createToken(t, "qlen", []string{"write"})

	body, _ := json.Marshal(map[string]any{"max_bytes": 1000, "max_objects": 0})
	resp := s.do(t, http.MethodPut, "/admin/buckets/qlen/quota", bytes.NewReader(body), "application/json")
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodPut, s.url+"/qlen/x.txt", strings.NewReader("hi"))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Del("Content-Length")
	resp, err = s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusLengthRequired {
		t.Fatalf("status=%d want=411", resp.StatusCode)
	}
}
