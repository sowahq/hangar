package s3

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/accesslog"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
)

func TestS3BucketLoggingXML(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "logb"}); err != nil {
		t.Fatalf("src: %v", err)
	}
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "logtarget"}); err != nil {
		t.Fatalf("tgt: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/logb", "logging=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get empty: %d body=%s", resp.StatusCode, body)
	}
	var initial BucketLoggingStatusXML
	if err := xml.Unmarshal(body, &initial); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if initial.LoggingEnabled != nil {
		t.Fatalf("expected empty initial config")
	}

	putBody := []byte(`<BucketLoggingStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><LoggingEnabled><TargetBucket>logtarget</TargetBucket><TargetPrefix>logs/</TargetPrefix></LoggingEnabled></BucketLoggingStatus>`)
	resp = s.do(t, http.MethodPut, "/logb", "logging=", putBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/logb", "logging=", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var got BucketLoggingStatusXML
	if err := xml.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode2: %v body=%s", err, body)
	}
	if got.LoggingEnabled == nil || got.LoggingEnabled.TargetBucket != "logtarget" || got.LoggingEnabled.TargetPrefix != "logs/" {
		t.Fatalf("got %+v", got.LoggingEnabled)
	}

	disableBody := []byte(`<BucketLoggingStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
	resp = s.do(t, http.MethodPut, "/logb", "logging=", disableBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable: %d", resp.StatusCode)
	}
	resp = s.do(t, http.MethodGet, "/logb", "logging=", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var after BucketLoggingStatusXML
	_ = xml.Unmarshal(body, &after)
	if after.LoggingEnabled != nil {
		t.Fatalf("expected disabled")
	}
}

func TestS3BucketLoggingWriter(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "wsrc"}); err != nil {
		t.Fatalf("src: %v", err)
	}
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "wtgt"}); err != nil {
		t.Fatalf("tgt: %v", err)
	}
	if err := bucket.PutLogging("wsrc", &bucket.LoggingConfig{TargetBucket: "wtgt", TargetPrefix: "access/"}); err != nil {
		t.Fatalf("put logging: %v", err)
	}

	accesslog.Start()
	t.Cleanup(accesslog.Stop)

	resp := s.do(t, http.MethodPut, "/wsrc/key1", "", []byte("hello"))
	resp.Body.Close()
	resp = s.do(t, http.MethodGet, "/wsrc/key1", "", nil)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	deadline := time.Now().Add(8 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		res, err := object.ListObjectsV2(&object.ListObjectsV2Request{Bucket: "wtgt", Prefix: "access/", MaxKeys: 100})
		if err == nil && len(res.Objects) > 0 {
			for _, obj := range res.Objects {
				gr, gerr := object.GetObject(&object.GetObjectRequest{Bucket: "wtgt", Key: obj.Key})
				if gerr != nil {
					continue
				}
				body, _ := io.ReadAll(gr.Reader)
				if strings.Contains(string(body), "wsrc") && strings.Contains(string(body), "PUT") {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected access log entry in target bucket")
	}
}
