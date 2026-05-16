package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func resetRegistry(t *testing.T) {
	t.Helper()
	ResetForTest()
	Init()
}

func TestMiddlewareRecordsRequest(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		handler      fiber.Handler
		method       string
		wantSubstr   []string
		wantNotEmpty bool
	}{
		{
			name:   "200 GET",
			path:   "/ok",
			method: "GET",
			handler: func(c *fiber.Ctx) error {
				return c.SendString("ok")
			},
			wantSubstr: []string{
				`hangar_requests_total{api="http",method="GET",status="200"} 1`,
				`hangar_request_duration_seconds_count{api="http",method="GET",status="200"} 1`,
			},
		},
		{
			name:   "500 PUT",
			path:   "/boom",
			method: "PUT",
			handler: func(c *fiber.Ctx) error {
				return fiber.NewError(fiber.StatusInternalServerError, "boom")
			},
			wantSubstr: []string{
				`hangar_requests_total{api="http",method="PUT",status="500"} 1`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetRegistry(t)

			app := fiber.New(fiber.Config{DisableStartupMessage: true})
			app.Use(Middleware("http"))
			app.All(tc.path, tc.handler)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			_ = resp.Body.Close()

			body := scrapeMetrics(t)
			for _, s := range tc.wantSubstr {
				if !strings.Contains(body, s) {
					t.Errorf("metrics body missing %q\nfull body:\n%s", s, body)
				}
			}
		})
	}
}

func TestObserveGC(t *testing.T) {
	resetRegistry(t)

	ObserveGC(10, 3, 2, 4096, time.Unix(1700000000, 0))
	ObserveGC(11, 1, 1, 1024, time.Unix(1700000100, 0))

	body := scrapeMetrics(t)

	cases := []struct {
		name string
		want string
	}{
		{"total_chunks", `hangar_gc_total_chunks 11`},
		{"orphan_chunks", `hangar_gc_orphan_chunks 1`},
		{"deleted cumulative", `hangar_gc_deleted_chunks_total 3`},
		{"freed cumulative", `hangar_gc_freed_bytes_total 5120`},
		{"last tick", `hangar_gc_last_tick_seconds 1.7000001e+09`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(body, tc.want) {
				t.Errorf("missing %q\nbody:\n%s", tc.want, body)
			}
		})
	}
}

func TestObserveScrubAndDisk(t *testing.T) {
	resetRegistry(t)

	ObserveScrub(2, 1, 8192, 4, 0, time.Unix(1700000200, 0))
	ObserveDisk(1024, 4096, 512, 0)

	body := scrapeMetrics(t)

	cases := []struct {
		name string
		want string
	}{
		{"corrupted", `hangar_scrub_corrupted_total 2`},
		{"quarantined", `hangar_scrub_quarantined_total 1`},
		{"bytes scanned", `hangar_scrub_bytes_scanned_total 8192`},
		{"missing files", `hangar_scrub_missing_files 4`},
		{"disk free", `hangar_disk_free_bytes 1024`},
		{"disk total", `hangar_disk_total_bytes 4096`},
		{"node used", `hangar_disk_node_used_bytes 512`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(body, tc.want) {
				t.Errorf("missing %q\nbody:\n%s", tc.want, body)
			}
		})
	}
}

func TestMultipartInflight(t *testing.T) {
	resetRegistry(t)

	MultipartInflightInc()
	MultipartInflightInc()
	MultipartInflightDec()

	body := scrapeMetrics(t)

	if !strings.Contains(body, "hangar_multipart_inflight 1") {
		t.Errorf("expected multipart inflight=1\nbody:\n%s", body)
	}
}

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/metrics", Handler())

	req := httptest.NewRequest("GET", "/metrics", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}
