package http

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeepHealthCheck(t *testing.T) {
	s := newTestServer(t)
	resp := s.do(t, http.MethodGet, "/status", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var out struct {
		Status string `json:"status"`
		DB     bool   `json:"db"`
		Checks []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("status=%s want=ok", out.Status)
	}
	if !out.DB {
		t.Fatalf("db check should pass")
	}
	if len(out.Checks) == 0 {
		t.Fatalf("expected checks present")
	}
}
