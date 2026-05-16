package bucket

import "testing"

func TestMatchCORS(t *testing.T) {
	cfg := &CORSConfiguration{
		Rules: []CORSRule{
			{
				AllowedOrigins: []string{"https://app.example.com"},
				AllowedMethods: []string{"GET", "PUT"},
				AllowedHeaders: []string{"x-amz-*", "content-type"},
			},
			{
				AllowedOrigins: []string{"*"},
				AllowedMethods: []string{"GET"},
			},
		},
	}

	tests := []struct {
		name    string
		origin  string
		method  string
		headers []string
		want    bool
		wantIdx int
	}{
		{name: "exact-origin-method", origin: "https://app.example.com", method: "GET", want: true, wantIdx: 0},
		{name: "wildcard-origin-fallback", origin: "https://other.com", method: "GET", want: true, wantIdx: 1},
		{name: "method-not-allowed", origin: "https://other.com", method: "PUT", want: false},
		{name: "allowed-headers-glob", origin: "https://app.example.com", method: "PUT", headers: []string{"x-amz-meta-foo"}, want: true, wantIdx: 0},
		{name: "header-not-allowed", origin: "https://app.example.com", method: "PUT", headers: []string{"authorization"}, want: false},
		{name: "empty-origin", origin: "", method: "GET", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, ok := MatchCORS(cfg, tt.origin, tt.method, tt.headers)
			if ok != tt.want {
				t.Fatalf("ok=%v want %v", ok, tt.want)
			}
			if ok && &cfg.Rules[tt.wantIdx] != rule {
				t.Fatalf("matched wrong rule")
			}
		})
	}
}

func TestOriginMatches(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		origin  string
		want    bool
	}{
		{name: "exact", pattern: "https://a.com", origin: "https://a.com", want: true},
		{name: "case-insensitive", pattern: "https://A.com", origin: "https://a.com", want: true},
		{name: "wildcard", pattern: "*", origin: "https://x", want: true},
		{name: "glob-prefix", pattern: "https://*.example.com", origin: "https://foo.example.com", want: true},
		{name: "glob-mismatch", pattern: "https://*.example.com", origin: "https://example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := originMatches([]string{tt.pattern}, tt.origin)
			if got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
