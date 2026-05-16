package s3

import (
	"encoding/xml"
	"net/http"
	"testing"
)

func TestS3LifecyclePutGetDelete(t *testing.T) {
	s := newS3TestServer(t)

	if r := s.do(t, http.MethodPut, "/lcb", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}

	cfg := lifecycleConfigurationXML{Rules: []lifecycleRuleXML{{
		ID:         "expire-tmp",
		Status:     "Enabled",
		Filter:     &lifecycleFilterXML{Prefix: "tmp/"},
		Expiration: &lifecycleExpirationXML{Days: 30},
	}}}
	body, _ := xml.Marshal(cfg)

	if r := s.do(t, http.MethodPut, "/lcb", "lifecycle=", body); r.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", r.StatusCode)
	}

	resp := s.do(t, http.MethodGet, "/lcb", "lifecycle=", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var got lifecycleConfigurationXML
	if err := xml.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(got.Rules) != 1 || got.Rules[0].Expiration.Days != 30 {
		t.Fatalf("got=%+v", got)
	}

	if r := s.do(t, http.MethodDelete, "/lcb", "lifecycle=", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}

	if r := s.do(t, http.MethodGet, "/lcb", "lifecycle=", nil); r.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: %d", r.StatusCode)
	}
}

func TestS3LifecycleRejectsEmpty(t *testing.T) {
	s := newS3TestServer(t)
	if r := s.do(t, http.MethodPut, "/lcb2", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", r.StatusCode)
	}

	tests := []struct {
		name string
		body []byte
		want int
	}{
		{name: "empty", body: nil, want: http.StatusBadRequest},
		{name: "no-rules", body: []byte(`<LifecycleConfiguration/>`), want: http.StatusBadRequest},
		{name: "rule-no-action", body: []byte(`<LifecycleConfiguration><Rule><Status>Enabled</Status></Rule></LifecycleConfiguration>`), want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := s.do(t, http.MethodPut, "/lcb2", "lifecycle=", tt.body)
			if r.StatusCode != tt.want {
				t.Fatalf("status=%d want=%d", r.StatusCode, tt.want)
			}
			r.Body.Close()
		})
	}
}
