package s3

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"testing"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func TestS3UploadPartCopy(t *testing.T) {
	s := newS3TestServer(t)

	for _, b := range []string{"upcsrc", "upcdst"} {
		if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: b}); err != nil {
			t.Fatalf("seed %s: %v", b, err)
		}
	}

	srcPayload := bytes.Repeat([]byte("A"), 1024)
	resp := s.do(t, http.MethodPut, "/upcsrc/source.bin", "", srcPayload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put src: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodPost, "/upcdst/copy.bin", "uploads=", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initiate: %d body=%s", resp.StatusCode, body)
	}
	var init InitiateMultipartUploadResult
	if err := xml.Unmarshal(body, &init); err != nil {
		t.Fatalf("decode init: %v", err)
	}

	tests := []struct {
		name        string
		partNumber  int
		copySource  string
		rangeHeader string
		wantStatus  int
		wantSize    int64
	}{
		{"full copy", 1, "/upcsrc/source.bin", "", http.StatusOK, 1024},
		{"range copy", 2, "/upcsrc/source.bin", "bytes=0-511", http.StatusOK, 512},
		{"missing source", 3, "/upcsrc/missing.bin", "", http.StatusNotFound, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := s.sign(t, http.MethodPut, "/upcdst/copy.bin", "partNumber="+itoa(tt.partNumber)+"&uploadId="+init.UploadID, nil)
			req.Header.Set("x-amz-copy-source", tt.copySource)
			if tt.rangeHeader != "" {
				req.Header.Set("x-amz-copy-source-range", tt.rangeHeader)
			}
			r, err := s.client.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			rb, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != tt.wantStatus {
				t.Fatalf("status=%d want %d body=%s", r.StatusCode, tt.wantStatus, rb)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var out CopyPartResult
			if err := xml.Unmarshal(rb, &out); err != nil {
				t.Fatalf("decode: %v body=%s", err, rb)
			}
			if out.ETag == "" {
				t.Fatalf("empty etag")
			}
		})
	}

	completeXML := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>x</ETag></Part><Part><PartNumber>2</PartNumber><ETag>y</ETag></Part></CompleteMultipartUpload>`
	resp = s.do(t, http.MethodPost, "/upcdst/copy.bin", "uploadId="+init.UploadID, []byte(completeXML))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/upcdst/copy.bin", "", nil)
	gb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if len(gb) != 1024+512 {
		t.Fatalf("expected combined size %d, got %d", 1024+512, len(gb))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
