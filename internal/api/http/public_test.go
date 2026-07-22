package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestCreateBucketPublicFlag(t *testing.T) {
	s := newTestServer(t)

	body, _ := json.Marshal(map[string]any{"public": true})
	resp := s.do(t, http.MethodPut, "/admin/buckets/pubc", bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	tok := s.createToken(t, "pubc", []string{"write"})
	resp = s.doAuth(t, http.MethodPut, "/pubc/a.txt", tok, bytes.NewReader([]byte("hello")), "application/octet-stream")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/pubc/a.txt", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous read on public bucket status=%d", resp.StatusCode)
	}
}

func TestUpdateBucketPublicToggle(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "pubt")
	tok := s.createToken(t, "pubt", []string{"write"})

	resp := s.doAuth(t, http.MethodPut, "/pubt/a.txt", tok, bytes.NewReader([]byte("hello")), "application/octet-stream")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d", resp.StatusCode)
	}

	tests := []struct {
		name       string
		public     bool
		wantStatus int
	}{
		{name: "made public allows anonymous read", public: true, wantStatus: http.StatusOK},
		{name: "made private again rejects anonymous read", public: false, wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"public": tt.public})
			resp := s.do(t, http.MethodPut, "/admin/buckets/pubt/public", bytes.NewReader(body), "application/json")
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("toggle status=%d", resp.StatusCode)
			}

			resp = s.do(t, http.MethodGet, "/pubt/a.txt", nil, "")
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("anonymous read status=%d want=%d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
