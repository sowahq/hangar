package s3

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3AnonymousAccess(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "pub", Public: true}); err != nil {
		t.Fatalf("create pub: %v", err)
	}
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "priv"}); err != nil {
		t.Fatalf("create priv: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/pub/hello.txt", "", []byte("hello"))
	resp.Body.Close()
	resp = s.do(t, http.MethodPut, "/priv/secret.txt", "", []byte("secret"))
	resp.Body.Close()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantCode   string
	}{
		{name: "get public object", method: http.MethodGet, path: "/pub/hello.txt", wantStatus: http.StatusOK, wantBody: "hello"},
		{name: "head public object", method: http.MethodHead, path: "/pub/hello.txt", wantStatus: http.StatusOK},
		{name: "get missing key public bucket", method: http.MethodGet, path: "/pub/nope.txt", wantStatus: http.StatusNotFound, wantCode: "NoSuchKey"},
		{name: "get private object", method: http.MethodGet, path: "/priv/secret.txt", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "head private object", method: http.MethodHead, path: "/priv/secret.txt", wantStatus: http.StatusForbidden},
		{name: "get unknown bucket", method: http.MethodGet, path: "/ghost/x", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "list public bucket denied", method: http.MethodGet, path: "/pub", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "list buckets denied", method: http.MethodGet, path: "/", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "put denied", method: http.MethodPut, path: "/pub/new.txt", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "delete denied", method: http.MethodDelete, path: "/pub/hello.txt", wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, s.url+tc.path, strings.NewReader(""))
			if err != nil {
				t.Fatalf("request: %v", err)
			}

			r, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()

			if r.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%q", r.StatusCode, tc.wantStatus, body)
			}
			if tc.wantBody != "" && string(body) != tc.wantBody {
				t.Fatalf("body=%q want=%q", body, tc.wantBody)
			}
			if tc.wantCode != "" && !strings.Contains(string(body), "<Code>"+tc.wantCode+"</Code>") {
				t.Fatalf("body=%q want code=%s", body, tc.wantCode)
			}
		})
	}
}

func TestS3AnonymousSubresourceDenied(t *testing.T) {
	s := newS3TestServer(t)

	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "pubsub", Public: true}); err != nil {
		t.Fatalf("create: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/pubsub/o.txt", "", []byte("x"))
	resp.Body.Close()

	queries := []string{"tagging", "acl", "attributes", "policy", "notification", "requestPayment", "versioning", "location"}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			path := "/pubsub/o.txt"
			if q == "policy" || q == "notification" || q == "requestPayment" || q == "versioning" || q == "location" {
				path = "/pubsub"
			}

			r, err := s.client.Get(s.url + path + "?" + q)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()

			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("%s: status=%d body=%q", q, r.StatusCode, body)
			}
		})
	}
}
