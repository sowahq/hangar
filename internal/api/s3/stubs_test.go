package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3StubsBucket(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "stubb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		query      string
		body       []byte
		wantStatus int
	}{
		{"get acl", http.MethodGet, "acl=", nil, http.StatusOK},
		{"put acl", http.MethodPut, "acl=", []byte("<x/>"), http.StatusOK},
		{"get policy 404", http.MethodGet, "policy=", nil, http.StatusNotFound},
		{"put policy", http.MethodPut, "policy=", []byte("{}"), http.StatusNoContent},
		{"delete policy", http.MethodDelete, "policy=", nil, http.StatusNoContent},
		{"delete website", http.MethodDelete, "website=", nil, http.StatusNoContent},
		{"get notification", http.MethodGet, "notification=", nil, http.StatusOK},
		{"put notification", http.MethodPut, "notification=", []byte("<x/>"), http.StatusOK},
		{"get requestPayment", http.MethodGet, "requestPayment=", nil, http.StatusOK},
		{"put requestPayment", http.MethodPut, "requestPayment=", []byte("<x/>"), http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := s.do(t, tt.method, "/stubb", tt.query, tt.body)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestS3StubsObjectACL(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "objaclb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/objaclb/k", "", []byte("x"))
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/objaclb/k", "acl=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get acl status=%d", resp.StatusCode)
	}
	var out AccessControlPolicyXML
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if len(out.AccessControlList) != 1 || out.AccessControlList[0].Permission != "FULL_CONTROL" {
		t.Fatalf("bad ACL: %+v", out)
	}

	resp = s.do(t, http.MethodPut, "/objaclb/k", "acl=", []byte("<x/>"))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put acl status=%d", resp.StatusCode)
	}
}
