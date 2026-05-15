package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestAuthRequiredForPrivateBucket(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "priv")

	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
		want   int
	}{
		{name: "PUT without token", method: http.MethodPut, path: "/priv/x.txt", body: bytes.NewReader([]byte("d")), want: http.StatusUnauthorized},
		{name: "GET without token", method: http.MethodGet, path: "/priv/x.txt", body: nil, want: http.StatusUnauthorized},
		{name: "DELETE without token", method: http.MethodDelete, path: "/priv/x.txt", body: nil, want: http.StatusUnauthorized},
		{name: "list without token", method: http.MethodGet, path: "/priv", body: nil, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.do(t, tt.method, tt.path, tt.body, "application/octet-stream")
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tt.want)
			}
		})
	}
}

func TestPublicBucketGetSkipsAuth(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "pubb")

	if _, err := bucket.UpdateQuota("pubb", 0, 0); err != nil {
		t.Fatalf("seed quota: %v", err)
	}
	info, err := bucket.GetBucket("pubb")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	info.Public = true
	data, _ := json.Marshal(info)
	if err := persistBucketRaw(t, "pubb", data); err != nil {
		t.Fatalf("persist: %v", err)
	}

	tok := s.createToken(t, "pubb", []string{"write"})
	resp := s.doAuth(t, http.MethodPut, "/pubb/o.txt", tok, bytes.NewReader([]byte("hello")), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("seed upload status=%d body=%s", resp.StatusCode, string(b))
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/pubb/o.txt", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public GET status=%d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !bytes.Equal(body, []byte("hello")) {
		t.Fatalf("body mismatch: %q", body)
	}

	resp = s.do(t, http.MethodPut, "/pubb/y.txt", bytes.NewReader([]byte("z")), "application/octet-stream")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("public PUT should still need auth, got %d", resp.StatusCode)
	}
}

func TestTokenAdminEndpoints(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "tokb")

	body, _ := json.Marshal(map[string]any{"permissions": []string{"read", "write"}})
	resp := s.do(t, http.MethodPost, "/admin/buckets/tokb/tokens", bytes.NewReader(body), "application/json")
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create token status=%d body=%s", resp.StatusCode, string(b))
	}
	var created struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(readBody(t, resp), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Token == "" || created.ID == "" {
		t.Fatalf("missing token/id")
	}

	resp = s.do(t, http.MethodGet, "/admin/buckets/tokb/tokens", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var out struct {
		Count  int `json:"count"`
		Tokens []struct {
			ID string `json:"id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if out.Count != 1 || out.Tokens[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", out)
	}

	resp = s.do(t, http.MethodDelete, "/admin/buckets/tokb/tokens/"+created.ID, nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d want=204", resp.StatusCode)
	}

	resp = s.do(t, http.MethodDelete, "/admin/buckets/tokb/tokens/"+created.ID, nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete status=%d want=404", resp.StatusCode)
	}
}
