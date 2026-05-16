package s3

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

func newTestCtx(headers map[string]string) (*fiber.App, *fiber.Ctx) {
	app := fiber.New()
	fctx := &fasthttp.RequestCtx{}
	for k, v := range headers {
		fctx.Request.Header.Set(k, v)
	}
	return app, app.AcquireCtx(fctx)
}

func TestParseChecksum(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		wantAlgo string
		wantVal  string
	}{
		{name: "none", headers: nil, wantAlgo: "", wantVal: ""},
		{name: "crc32", headers: map[string]string{"x-amz-checksum-crc32": "AAAAAA=="}, wantAlgo: "crc32", wantVal: "AAAAAA=="},
		{name: "sha256", headers: map[string]string{"x-amz-checksum-sha256": "ZXh4"}, wantAlgo: "sha256", wantVal: "ZXh4"},
		{name: "hint-only", headers: map[string]string{"x-amz-sdk-checksum-algorithm": "CRC32C"}, wantAlgo: "crc32c", wantVal: ""},
		{name: "value-wins-over-hint", headers: map[string]string{"x-amz-checksum-sha1": "abc=", "x-amz-sdk-checksum-algorithm": "CRC32"}, wantAlgo: "sha1", wantVal: "abc="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, ctx := newTestCtx(tt.headers)
			defer app.ReleaseCtx(ctx)

			algo, val := parseChecksum(ctx)
			if algo != tt.wantAlgo || val != tt.wantVal {
				t.Fatalf("got (%q,%q) want (%q,%q)", algo, val, tt.wantAlgo, tt.wantVal)
			}
		})
	}
}

func TestWriteChecksumHeaders(t *testing.T) {
	tests := []struct {
		name      string
		algo      string
		value     string
		wantSet   bool
		wantValue string
	}{
		{name: "empty", algo: "", value: "", wantSet: false},
		{name: "algo-no-value", algo: "crc32", value: "", wantSet: false},
		{name: "set", algo: "sha256", value: "ZXh4", wantSet: true, wantValue: "ZXh4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, ctx := newTestCtx(nil)
			defer app.ReleaseCtx(ctx)

			writeChecksumHeaders(ctx, tt.algo, tt.value)
			got := string(ctx.Response().Header.Peek("x-amz-checksum-" + tt.algo))
			gotType := string(ctx.Response().Header.Peek("x-amz-checksum-type"))

			if tt.wantSet {
				if got != tt.wantValue {
					t.Fatalf("checksum header = %q want %q", got, tt.wantValue)
				}
				if gotType != "FULL_OBJECT" {
					t.Fatalf("checksum-type = %q want FULL_OBJECT", gotType)
				}
			} else {
				if got != "" || gotType != "" {
					t.Fatalf("expected no headers, got value=%q type=%q", got, gotType)
				}
			}
		})
	}
}
