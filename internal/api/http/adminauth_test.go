package http

import (
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/config"
)

func TestAdminTokenMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		header     string
		wantStatus int
	}{
		{
			name:       "no token configured allows request",
			configured: "",
			header:     "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "token configured missing header is rejected",
			configured: "s3cr3t",
			header:     "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token configured wrong token is rejected",
			configured: "s3cr3t",
			header:     "Bearer nope",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token configured non-bearer scheme is rejected",
			configured: "s3cr3t",
			header:     "s3cr3t",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "token configured correct token is accepted",
			configured: "s3cr3t",
			header:     "Bearer s3cr3t",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)

			config.SetAdminTokenForTest(tt.configured)
			t.Cleanup(func() { config.SetAdminTokenForTest("") })

			req, err := http.NewRequest(http.MethodGet, s.url+"/admin/buckets", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			resp, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
