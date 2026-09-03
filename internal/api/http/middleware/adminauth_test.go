package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/config"
)

func TestRequireAdminToken(t *testing.T) {
	tests := []struct {
		name        string
		configured  string
		allowUnauth bool
		header      string
		wantStatus  int
	}{
		{
			name:       "no token configured denies request",
			configured: "",
			header:     "",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:        "no token but explicit opt-in allows request",
			configured:  "",
			allowUnauth: true,
			header:      "",
			wantStatus:  http.StatusOK,
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
			config.SetAdminTokenForTest(tt.configured)
			config.SetAllowUnauthenticatedAdminForTest(tt.allowUnauth)
			t.Cleanup(func() {
				config.SetAdminTokenForTest("")
				config.SetAllowUnauthenticatedAdminForTest(false)
			})

			app := fiber.New(fiber.Config{ErrorHandler: response.ErrorHandler})
			app.Get("/admin/ping", RequireAdminToken(), func(c *fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
