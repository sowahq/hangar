package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3BucketWebsiteCRUD(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "webb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/webb", "website=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 initial: %d", resp.StatusCode)
	}

	put := []byte(`<WebsiteConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IndexDocument><Suffix>index.html</Suffix></IndexDocument><ErrorDocument><Key>error.html</Key></ErrorDocument></WebsiteConfiguration>`)
	resp = s.do(t, http.MethodPut, "/webb", "website=", put)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/webb", "website=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var got WebsiteConfigurationXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.IndexDocument == nil || got.IndexDocument.Suffix != "index.html" {
		t.Fatalf("index mismatch: %+v", got)
	}
	if got.ErrorDocument == nil || got.ErrorDocument.Key != "error.html" {
		t.Fatalf("error mismatch: %+v", got)
	}

	resp = s.do(t, http.MethodDelete, "/webb", "website=", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
}

func TestS3BucketWebsiteAnonymousServe(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "webserve", Public: true}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	if err := bucket.PutWebsite("webserve", &bucket.WebsiteConfig{IndexDocument: "index.html", ErrorDocument: "error.html"}); err != nil {
		t.Fatalf("put website: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/webserve/index.html", "", []byte("<h1>home</h1>"))
	resp.Body.Close()
	resp = s.do(t, http.MethodPut, "/webserve/error.html", "", []byte("<h1>404</h1>"))
	resp.Body.Close()

	// anonymous GET root → index
	r, err := s.client.Get(s.url + "/webserve")
	if err != nil {
		t.Fatalf("GET root: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || string(body) != "<h1>home</h1>" {
		t.Fatalf("root status=%d body=%q", r.StatusCode, body)
	}

	// anonymous GET missing → error page 404
	r, err = s.client.Get(s.url + "/webserve/missing.html")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound || string(body) != "<h1>404</h1>" {
		t.Fatalf("missing status=%d body=%q", r.StatusCode, body)
	}

	// anonymous GET present key → its content
	r, err = s.client.Get(s.url + "/webserve/index.html")
	if err != nil {
		t.Fatalf("GET index.html: %v", err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || string(body) != "<h1>home</h1>" {
		t.Fatalf("direct index status=%d body=%q", r.StatusCode, body)
	}
}
