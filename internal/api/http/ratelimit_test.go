package http

import (
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/config"
)

func TestRateLimitBurst(t *testing.T) {
	config.SetRateLimitForTest(true, 3, 60)
	t.Cleanup(func() { config.SetRateLimitForTest(false, 100, 60) })

	s := newTestServer(t)

	var got429 bool
	for i := 0; i < 10; i++ {
		resp := s.do(t, http.MethodGet, "/status", nil, "")
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatalf("expected at least one 429 within burst")
	}
}
