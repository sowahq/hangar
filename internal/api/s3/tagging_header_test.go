package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestS3PutObjectWithTaggingHeader(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "thb"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	req := s.sign(t, http.MethodPut, "/thb/k", "", []byte("body"))
	req.Header.Set("x-amz-tagging", "env=prod&team=platform")
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodHead, "/thb/k", "", nil)
	resp.Body.Close()
	if got := resp.Header.Get("x-amz-tagging-count"); got != "2" {
		t.Fatalf("tagging-count=%q want 2", got)
	}

	resp = s.do(t, http.MethodGet, "/thb/k", "tagging=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var got TaggingXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TagSet) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(got.TagSet))
	}
}

func TestS3PutObjectTaggingHeaderInvalid(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "thbi"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	tooMany := ""
	for i := 0; i < 11; i++ {
		if i > 0 {
			tooMany += "&"
		}
		tooMany += "k" + string(rune('a'+i)) + "=v"
	}

	req := s.sign(t, http.MethodPut, "/thbi/k", "", []byte("x"))
	req.Header.Set("x-amz-tagging", tooMany)
	resp, _ := s.client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for >10 tags, got %d", resp.StatusCode)
	}
}
