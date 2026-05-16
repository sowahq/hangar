package s3

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"testing"
)

func TestS3CORSPutGetDelete(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/corsb", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create bucket: %d", r.StatusCode)
	}

	cfg := CORSConfigurationXML{
		Rules: []CORSRuleXML{{
			AllowedOrigins: []string{"https://app.example.com"},
			AllowedMethods: []string{"GET", "PUT"},
			AllowedHeaders: []string{"*"},
			ExposeHeaders:  []string{"ETag"},
			MaxAgeSeconds:  60,
		}},
	}
	body, _ := xml.Marshal(cfg)

	resp := s.do(t, http.MethodPut, "/corsb", "cors=", body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("put cors: %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/corsb", "cors=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get cors: %d", resp.StatusCode)
	}
	var got CORSConfigurationXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(got.Rules) != 1 || got.Rules[0].MaxAgeSeconds != 60 {
		t.Fatalf("unexpected cors: %+v", got)
	}

	resp = s.do(t, http.MethodDelete, "/corsb", "cors=", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete cors: %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/corsb", "cors=", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get cors after delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestS3CORSPreflight(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/corsp", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}

	cfg := CORSConfigurationXML{Rules: []CORSRuleXML{{
		AllowedOrigins: []string{"https://app.example.com"},
		AllowedMethods: []string{"PUT"},
		AllowedHeaders: []string{"x-amz-*"},
	}}}
	body, _ := xml.Marshal(cfg)
	if r := s.do(t, http.MethodPut, "/corsp", "cors=", body); r.StatusCode != http.StatusOK {
		t.Fatalf("put cors: %d", r.StatusCode)
	}

	tests := []struct {
		name   string
		origin string
		method string
		hdrs   string
		want   int
	}{
		{name: "allowed", origin: "https://app.example.com", method: "PUT", hdrs: "x-amz-meta-foo", want: http.StatusOK},
		{name: "wrong-origin", origin: "https://evil.com", method: "PUT", want: http.StatusForbidden},
		{name: "wrong-method", origin: "https://app.example.com", method: "DELETE", want: http.StatusForbidden},
		{name: "missing-origin", origin: "", method: "PUT", want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodOptions, s.url+"/corsp", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			req.Header.Set("Access-Control-Request-Method", tt.method)
			if tt.hdrs != "" {
				req.Header.Set("Access-Control-Request-Headers", tt.hdrs)
			}
			resp, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tt.want)
			}
			if tt.want == http.StatusOK {
				if got := resp.Header.Get("Access-Control-Allow-Origin"); got != tt.origin {
					t.Fatalf("allow-origin=%q", got)
				}
				if got := resp.Header.Get("Access-Control-Allow-Methods"); got == "" {
					t.Fatalf("missing allow-methods")
				}
			}
		})
	}
}

func TestS3CORSResponseEcho(t *testing.T) {
	s := newS3TestServer(t)
	if r := s.do(t, http.MethodPut, "/corse", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}

	cfg := CORSConfigurationXML{Rules: []CORSRuleXML{{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		ExposeHeaders:  []string{"ETag"},
	}}}
	body, _ := xml.Marshal(cfg)
	if r := s.do(t, http.MethodPut, "/corse", "cors=", body); r.StatusCode != http.StatusOK {
		t.Fatalf("put cors: %d", r.StatusCode)
	}

	if r := s.do(t, http.MethodPut, "/corse/k", "", []byte("hi")); r.StatusCode != http.StatusOK {
		t.Fatalf("put obj: %d", r.StatusCode)
	}

	req := s.sign(t, http.MethodGet, "/corse/k", "", nil)
	req.Header.Set("Origin", "https://foo.bar")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get obj: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin=%q want *", got)
	}
	if got := resp.Header.Get("Access-Control-Expose-Headers"); got != "ETag" {
		t.Fatalf("expose=%q", got)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, []byte("hi")) {
		t.Fatalf("body=%q", got)
	}
}
