package s3

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func TestParseRangeClamp(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		size      int64
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{name: "end beyond size clamped", header: "bytes=0-99999999", size: 5, wantStart: 0, wantEnd: 4},
		{name: "exact range", header: "bytes=1-3", size: 5, wantStart: 1, wantEnd: 3},
		{name: "open end", header: "bytes=2-", size: 5, wantStart: 2, wantEnd: 4},
		{name: "start beyond size", header: "bytes=10-20", size: 5, wantErr: true},
		{name: "inverted", header: "bytes=3-1", size: 5, wantErr: true},
		{name: "suffix larger than size", header: "bytes=-100", size: 5, wantStart: 0, wantEnd: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := parseRange(tc.header, tc.size)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d-%d", start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("got %d-%d want %d-%d", start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

func TestParseCopySourceEncoding(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantKey string
	}{
		{name: "plus preserved", src: "/b/a+b.txt", wantKey: "a+b.txt"},
		{name: "encoded plus", src: "/b/a%2Bb.txt", wantKey: "a+b.txt"},
		{name: "encoded space", src: "/b/a%20b.txt", wantKey: "a b.txt"},
		{name: "plain", src: "/b/dir/file.txt", wantKey: "dir/file.txt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bkt, key, _, err := parseCopySource(tc.src)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bkt != "b" || key != tc.wantKey {
				t.Fatalf("got bucket=%q key=%q want bucket=b key=%q", bkt, key, tc.wantKey)
			}
		})
	}
}

func TestS3RangeGetClamped(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "rng"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/rng/f.txt", "", []byte("hello"))
	resp.Body.Close()

	req := s.sign(t, http.MethodGet, "/rng/f.txt", "", nil)
	req.Header.Set("Range", "bytes=0-99999999")
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	if r.StatusCode != http.StatusPartialContent {
		t.Fatalf("status=%d body=%q", r.StatusCode, body)
	}
	if string(body) != "hello" {
		t.Fatalf("body=%q", body)
	}
	if cr := r.Header.Get("Content-Range"); cr != "bytes 0-4/5" {
		t.Fatalf("content-range=%q", cr)
	}
}

func TestS3HeadObjectVersionID(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "headver"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	cfg := []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
	resp := s.do(t, http.MethodPut, "/headver", "versioning=", cfg)
	resp.Body.Close()

	resp = s.do(t, http.MethodPut, "/headver/doc.txt", "", []byte("version-one"))
	v1 := resp.Header.Get("x-amz-version-id")
	resp.Body.Close()

	resp = s.do(t, http.MethodPut, "/headver/doc.txt", "", []byte("v2"))
	resp.Body.Close()

	if v1 == "" {
		t.Fatal("no version id on first put")
	}

	resp = s.do(t, http.MethodHead, "/headver/doc.txt", "versionId="+v1, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head v1 status=%d", resp.StatusCode)
	}
	if cl := resp.Header.Get("Content-Length"); cl != strconv.Itoa(len("version-one")) {
		t.Fatalf("content-length=%q want %d", cl, len("version-one"))
	}

	resp = s.do(t, http.MethodHead, "/headver/doc.txt", "", nil)
	resp.Body.Close()
	if cl := resp.Header.Get("Content-Length"); cl != "2" {
		t.Fatalf("head latest content-length=%q want 2", cl)
	}
}

func TestS3ListVersionsPaginationComplete(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lvp"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	cfg := []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`)
	resp := s.do(t, http.MethodPut, "/lvp", "versioning=", cfg)
	resp.Body.Close()

	total := 0
	for _, key := range []string{"a.txt", "b.txt"} {
		for i := 0; i < 3; i++ {
			resp = s.do(t, http.MethodPut, "/lvp/"+key, "", []byte(fmt.Sprintf("%s-%d", key, i)))
			resp.Body.Close()
			total++
		}
	}

	seen := map[string]int{}
	keyMarker := ""
	vidMarker := ""

	for page := 0; page < 10; page++ {
		q := "versions=&max-keys=2"
		if keyMarker != "" {
			q += "&key-marker=" + keyMarker + "&version-id-marker=" + vidMarker
		}

		resp = s.do(t, http.MethodGet, "/lvp", q, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d status=%d body=%q", page, resp.StatusCode, body)
		}

		var out ListVersionsResult
		if err := xml.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode: %v", err)
		}

		for _, v := range out.Versions {
			seen[v.Key+"/"+v.VersionID]++
		}

		if !out.IsTruncated {
			break
		}
		keyMarker = out.NextKeyMarker
		vidMarker = out.NextVersionIDMarker
		if keyMarker == "" {
			t.Fatal("truncated without next key marker")
		}
	}

	if len(seen) != total {
		t.Fatalf("saw %d distinct versions, want %d: %v", len(seen), total, seen)
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("version %s seen %d times", k, n)
		}
	}
}

func chunkedSetup(t *testing.T, s *s3TestServer, bucketName, path, payloadHash string, decodedLen int) (http.Header, string, []byte, string, string) {
	t.Helper()
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: bucketName}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	amzDate := s.now.Format("20060102T150405Z")
	date := s.now.Format("20060102")
	region := "us-east-1"

	signedHeaders := []string{"content-encoding", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-decoded-content-length"}
	headers := http.Header{}
	headers.Set("Host", s.host)
	headers.Set("X-Amz-Date", amzDate)
	headers.Set("X-Amz-Content-Sha256", payloadHash)
	headers.Set("Content-Encoding", "aws-chunked")
	headers.Set("X-Amz-Decoded-Content-Length", strconv.Itoa(decodedLen))

	sigReq := &Request{Method: http.MethodPut, Path: path, Headers: headers}
	cr, _, err := CanonicalRequest(sigReq, signedHeaders, payloadHash)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	sts := StringToSign(amzDate, date, region, "s3", sha256Hex(cr))
	signingKey := DeriveSigningKey(s.key.SecretKey, date, region, "s3")
	seedSig := Sign(sts, signingKey)

	authHeader := "AWS4-HMAC-SHA256 " +
		"Credential=" + s.key.AccessKeyID + "/" + date + "/" + region + "/s3/aws4_request," +
		"SignedHeaders=" + strings.Join(signedHeaders, ";") + "," +
		"Signature=" + seedSig

	return headers, authHeader, signingKey, amzDate, date + "/" + region + "/s3/aws4_request"
}

func doChunkedPut(t *testing.T, s *s3TestServer, path string, headers http.Header, authHeader string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, s.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("Authorization", authHeader)

	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func TestS3AwsChunkedUnsignedTrailerPut(t *testing.T) {
	s := newS3TestServer(t)
	decoded := []byte("unsigned trailer payload body")

	headers, authHeader, _, _, _ := chunkedSetup(t, s, "utrail", "/utrail/u.txt", PayloadStreamingUnsignedTrailer, len(decoded))

	var body bytes.Buffer
	fmt.Fprintf(&body, "%x\r\n", len(decoded))
	body.Write(decoded)
	body.WriteString("\r\n")
	body.WriteString("0\r\n")
	body.WriteString("x-amz-checksum-crc32:AAAAAA==\r\n")
	body.WriteString("\r\n")

	resp := doChunkedPut(t, s, "/utrail/u.txt", headers, authHeader, body.Bytes())
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}

	resp = s.do(t, http.MethodGet, "/utrail/u.txt", "", nil)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, decoded) {
		t.Fatalf("payload mismatch: got %q want %q", got, decoded)
	}
}

func TestS3AwsChunkedSignedTrailerPut(t *testing.T) {
	s := newS3TestServer(t)
	decoded := []byte("signed trailer payload body!")

	headers, authHeader, signingKey, amzDate, scope := chunkedSetup(t, s, "strail", "/strail/st.txt", PayloadStreamingTrailer, len(decoded))

	seedSig := authHeader[strings.LastIndex(authHeader, "Signature=")+len("Signature="):]

	makeChunkSig := func(prev string, body []byte) string {
		h := sha256.Sum256(body)
		sts := "AWS4-HMAC-SHA256-PAYLOAD\n" + amzDate + "\n" + scope + "\n" + prev + "\n" + emptyStringSHA256 + "\n" + hex.EncodeToString(h[:])
		return Sign(sts, signingKey)
	}

	sig1 := makeChunkSig(seedSig, decoded)
	sig2 := makeChunkSig(sig1, nil)

	trailerLine := "x-amz-checksum-crc32:AAAAAA=="
	th := sha256.Sum256([]byte(trailerLine + "\n"))
	trailerSTS := "AWS4-HMAC-SHA256-TRAILER\n" + amzDate + "\n" + scope + "\n" + sig2 + "\n" + hex.EncodeToString(th[:])
	trailerSig := Sign(trailerSTS, signingKey)

	var body bytes.Buffer
	fmt.Fprintf(&body, "%x;chunk-signature=%s\r\n", len(decoded), sig1)
	body.Write(decoded)
	body.WriteString("\r\n")
	fmt.Fprintf(&body, "0;chunk-signature=%s\r\n", sig2)
	body.WriteString(trailerLine + "\r\n")
	body.WriteString("x-amz-trailer-signature:" + trailerSig + "\r\n")
	body.WriteString("\r\n")

	resp := doChunkedPut(t, s, "/strail/st.txt", headers, authHeader, body.Bytes())
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}

	resp = s.do(t, http.MethodGet, "/strail/st.txt", "", nil)
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(got, decoded) {
		t.Fatalf("payload mismatch: got %q want %q", got, decoded)
	}
}

func TestS3AwsChunkedMissingChunkSigRejected(t *testing.T) {
	s := newS3TestServer(t)
	decoded := []byte("payload without chunk sigs")

	headers, authHeader, _, _, _ := chunkedSetup(t, s, "nosig", "/nosig/n.txt", PayloadStreaming, len(decoded))

	var body bytes.Buffer
	fmt.Fprintf(&body, "%x\r\n", len(decoded))
	body.Write(decoded)
	body.WriteString("\r\n")
	body.WriteString("0\r\n\r\n")

	resp := doChunkedPut(t, s, "/nosig/n.txt", headers, authHeader, body.Bytes())
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("unsigned chunks accepted in signed streaming mode")
	}
}

func TestS3ETagIsMD5(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "etagmd5"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	content := []byte("hello")
	sum := md5.Sum(content)
	want := fmt.Sprintf("%q", hex.EncodeToString(sum[:]))

	resp := s.do(t, http.MethodPut, "/etagmd5/f.txt", "", content)
	resp.Body.Close()
	if got := resp.Header.Get("ETag"); got != want {
		t.Fatalf("put etag=%q want %q", got, want)
	}

	resp = s.do(t, http.MethodGet, "/etagmd5/f.txt", "", nil)
	resp.Body.Close()
	if got := resp.Header.Get("ETag"); got != want {
		t.Fatalf("get etag=%q want %q", got, want)
	}
}

func TestS3MultipartETagAWSFormula(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "mpetag"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/mpetag/f.bin", "uploads=", nil)
	var init InitiateMultipartUploadResult
	if err := xml.NewDecoder(resp.Body).Decode(&init); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	resp.Body.Close()

	part1 := bytes.Repeat([]byte("x"), 16)
	part2 := bytes.Repeat([]byte("y"), 16)

	combined := md5.New()
	for i, p := range [][]byte{part1, part2} {
		sum := md5.Sum(p)
		combined.Write(sum[:])

		q := "uploadId=" + init.UploadID + "&partNumber=" + strconv.Itoa(i+1)
		r := s.do(t, http.MethodPut, "/mpetag/f.bin", q, p)
		r.Body.Close()

		wantPart := fmt.Sprintf("%q", hex.EncodeToString(sum[:]))
		if got := r.Header.Get("ETag"); got != wantPart {
			t.Fatalf("part %d etag=%q want %q", i+1, got, wantPart)
		}
	}

	want := fmt.Sprintf("\"%s-2\"", hex.EncodeToString(combined.Sum(nil)))

	cb := &bytes.Buffer{}
	cb.WriteString("<CompleteMultipartUpload>")
	for i := 1; i <= 2; i++ {
		cb.WriteString("<Part><PartNumber>" + strconv.Itoa(i) + "</PartNumber></Part>")
	}
	cb.WriteString("</CompleteMultipartUpload>")

	resp = s.do(t, http.MethodPost, "/mpetag/f.bin", "uploadId="+init.UploadID, cb.Bytes())
	var done CompleteMultipartUploadResult
	if err := xml.NewDecoder(resp.Body).Decode(&done); err != nil {
		t.Fatalf("decode complete: %v", err)
	}
	resp.Body.Close()

	if done.ETag != want {
		t.Fatalf("complete etag=%q want %q", done.ETag, want)
	}
}

func TestS3PresignedExpiresOutOfRange(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "presrange"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	presign := func(expires string) int {
		amzDate := s.now.Format("20060102T150405Z")
		date := s.now.Format("20060102")
		region := "us-east-1"
		credential := s.key.AccessKeyID + "/" + date + "/" + region + "/s3/aws4_request"
		q := url.Values{}
		q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
		q.Set("X-Amz-Credential", credential)
		q.Set("X-Amz-Date", amzDate)
		q.Set("X-Amz-Expires", expires)
		q.Set("X-Amz-SignedHeaders", "host")
		rawQuery := q.Encode()
		sigReq := &Request{Method: http.MethodGet, Path: "/presrange/k", RawQuery: rawQuery, Headers: http.Header{}}
		sigReq.Headers.Set("Host", s.host)
		cr, _, _ := CanonicalRequest(sigReq, []string{"host"}, PayloadUnsigned)
		sts := StringToSign(amzDate, date, region, "s3", sha256Hex(cr))
		signingKey := DeriveSigningKey(s.key.SecretKey, date, region, "s3")
		q.Set("X-Amz-Signature", Sign(sts, signingKey))

		httpReq, _ := http.NewRequest(http.MethodGet, s.url+"/presrange/k?"+q.Encode(), nil)
		httpReq.Header.Set("Host", s.host)
		resp, err := s.client.Do(httpReq)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if st := presign("604801"); st != http.StatusBadRequest {
		t.Fatalf("expires too large: status=%d want 400", st)
	}
	if st := presign("0"); st != http.StatusBadRequest {
		t.Fatalf("expires zero: status=%d want 400", st)
	}
	if st := presign("-5"); st != http.StatusBadRequest {
		t.Fatalf("expires negative: status=%d want 400", st)
	}
}

func TestS3ListMaxKeysBounds(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "mkbounds"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	for _, k := range []string{"a", "b", "c"} {
		resp := s.do(t, http.MethodPut, "/mkbounds/"+k, "", []byte("x"))
		resp.Body.Close()
	}

	resp := s.do(t, http.MethodGet, "/mkbounds", "list-type=2&max-keys=0", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("max-keys=0 status=%d", resp.StatusCode)
	}
	var out ListBucketResultV2
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Contents) != 0 || out.IsTruncated {
		t.Fatalf("max-keys=0 must be empty non-truncated: contents=%d trunc=%v", len(out.Contents), out.IsTruncated)
	}

	resp = s.do(t, http.MethodGet, "/mkbounds", "list-type=2&max-keys=-1", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("max-keys=-1 status=%d want 400", resp.StatusCode)
	}

	resp = s.do(t, http.MethodGet, "/mkbounds", "list-type=2&max-keys=abc", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("max-keys=abc status=%d want 400", resp.StatusCode)
	}
}

func TestEncodeListValue(t *testing.T) {
	cases := []struct {
		enc, in, want string
	}{
		{"url", "a b/c", "a%20b/c"},
		{"url", "файл.txt", "%D1%84%D0%B0%D0%B9%D0%BB.txt"},
		{"url", "plain.txt", "plain.txt"},
		{"", "a b/c", "a b/c"},
	}
	for _, tc := range cases {
		if got := encodeListValue(tc.enc, tc.in); got != tc.want {
			t.Fatalf("encodeListValue(%q,%q)=%q want %q", tc.enc, tc.in, got, tc.want)
		}
	}
}

func TestS3ListEncodingTypeEcho(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "enctype"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/enctype/hello.txt", "", []byte("x"))
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/enctype", "list-type=2&encoding-type=url", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListBucketResultV2
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if out.EncodingType != "url" {
		t.Fatalf("EncodingType=%q want url", out.EncodingType)
	}
	if len(out.Contents) != 1 || out.Contents[0].Key != "hello.txt" {
		t.Fatalf("contents=%+v", out.Contents)
	}

	resp = s.do(t, http.MethodGet, "/enctype", "list-type=2&encoding-type=bogus", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid encoding-type status=%d want 400", resp.StatusCode)
	}
}

func TestS3CreateBucketIdempotent(t *testing.T) {
	s := newS3TestServer(t)

	resp := s.do(t, http.MethodPut, "/idem", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first create status=%d", resp.StatusCode)
	}

	resp = s.do(t, http.MethodPut, "/idem", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second create status=%d want 200", resp.StatusCode)
	}
}

func TestS3DeleteObjectUnknownBucket(t *testing.T) {
	s := newS3TestServer(t)
	resp := s.do(t, http.MethodDelete, "/nobucket/key", "", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404 body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "NoSuchBucket") {
		t.Fatalf("want NoSuchBucket body=%s", body)
	}
}

func TestS3ConditionalIfMatchPrecedence(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "cond"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPut, "/cond/o.txt", "", []byte("hello"))
	etag := resp.Header.Get("ETag")
	resp.Body.Close()

	req := s.sign(t, http.MethodGet, "/cond/o.txt", "", nil)
	req.Header.Set("If-Match", etag)
	req.Header.Set("If-Unmodified-Since", "Mon, 01 Jan 2000 00:00:00 GMT")
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("If-Match should win over If-Unmodified-Since: status=%d", r.StatusCode)
	}
	if string(body) != "hello" {
		t.Fatalf("body=%q", body)
	}

	req = s.sign(t, http.MethodGet, "/cond/o.txt", "", nil)
	req.Header.Set("If-None-Match", etag)
	r, err = s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match match should be 304: status=%d", r.StatusCode)
	}
	if r.Header.Get("ETag") != etag {
		t.Fatalf("304 must include ETag, got %q", r.Header.Get("ETag"))
	}
	if r.Header.Get("Last-Modified") == "" {
		t.Fatalf("304 must include Last-Modified")
	}
}

func TestS3CompleteMultipartValidation(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "cmpv"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/cmpv/f.bin", "uploads=", nil)
	var init InitiateMultipartUploadResult
	xml.NewDecoder(resp.Body).Decode(&init)
	resp.Body.Close()

	var etags []string
	for i := 1; i <= 2; i++ {
		q := "uploadId=" + init.UploadID + "&partNumber=" + strconv.Itoa(i)
		r := s.do(t, http.MethodPut, "/cmpv/f.bin", q, bytes.Repeat([]byte("z"), 16))
		etags = append(etags, r.Header.Get("ETag"))
		r.Body.Close()
	}

	resp = s.do(t, http.MethodPost, "/cmpv/f.bin", "uploadId="+init.UploadID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body status=%d want 400", resp.StatusCode)
	}

	badETag := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>"deadbeef"</ETag></Part></CompleteMultipartUpload>`
	resp = s.do(t, http.MethodPost, "/cmpv/f.bin", "uploadId="+init.UploadID, []byte(badETag))
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(rb), "InvalidPart") {
		t.Fatalf("bad etag status=%d body=%s", resp.StatusCode, rb)
	}

	descending := `<CompleteMultipartUpload><Part><PartNumber>2</PartNumber><ETag>` + etags[1] + `</ETag></Part><Part><PartNumber>1</PartNumber><ETag>` + etags[0] + `</ETag></Part></CompleteMultipartUpload>`
	resp = s.do(t, http.MethodPost, "/cmpv/f.bin", "uploadId="+init.UploadID, []byte(descending))
	rb, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(rb), "InvalidPartOrder") {
		t.Fatalf("descending order status=%d body=%s", resp.StatusCode, rb)
	}

	good := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>` + etags[0] + `</ETag></Part><Part><PartNumber>2</PartNumber><ETag>` + etags[1] + `</ETag></Part></CompleteMultipartUpload>`
	resp = s.do(t, http.MethodPost, "/cmpv/f.bin", "uploadId="+init.UploadID, []byte(good))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid complete status=%d", resp.StatusCode)
	}
}

func TestS3PayloadChecksumValidation(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "chksum"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	content := []byte("payload body")

	req := s.sign(t, http.MethodPut, "/chksum/bad-md5.txt", "", content)
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString([]byte("wrongwrongwrong!")))
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	rb, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest || !strings.Contains(string(rb), "BadDigest") {
		t.Fatalf("bad md5 status=%d body=%s", r.StatusCode, rb)
	}

	sum := md5.Sum(content)
	req = s.sign(t, http.MethodPut, "/chksum/good-md5.txt", "", content)
	req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	r, err = s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("good md5 status=%d", r.StatusCode)
	}
}

func TestS3RequestIDHeaders(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "reqid"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/reqid", "list-type=2", nil)
	resp.Body.Close()
	if resp.Header.Get("x-amz-request-id") == "" {
		t.Fatal("missing x-amz-request-id on success")
	}
	if resp.Header.Get("x-amz-id-2") == "" {
		t.Fatal("missing x-amz-id-2")
	}

	resp = s.do(t, http.MethodGet, "/nope-bucket", "list-type=2", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("x-amz-request-id") == "" {
		t.Fatal("missing x-amz-request-id on error")
	}
	if !strings.Contains(string(body), "<RequestId>") {
		t.Fatalf("error XML missing RequestId: %s", body)
	}
}

func TestS3TimestampMillis(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "tsms"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/tsms/o.txt", "", []byte("x"))
	resp.Body.Close()

	resp = s.do(t, http.MethodGet, "/tsms", "list-type=2", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListBucketResultV2
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Contents) != 1 {
		t.Fatalf("want 1 key got %d", len(out.Contents))
	}
	lm := out.Contents[0].LastModified
	if !strings.HasSuffix(lm, "Z") || !strings.Contains(lm, ".") {
		t.Fatalf("LastModified not ISO8601 millis: %q", lm)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", lm); err != nil {
		t.Fatalf("LastModified unparseable: %q err=%v", lm, err)
	}
}

func TestS3MultipleRangeReturnsFull(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "multrange"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	content := []byte("0123456789")
	resp := s.do(t, http.MethodPut, "/multrange/f.txt", "", content)
	resp.Body.Close()

	req := s.sign(t, http.MethodGet, "/multrange/f.txt", "", nil)
	req.Header.Set("Range", "bytes=0-1,5-9")
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	if r.StatusCode != http.StatusOK {
		t.Fatalf("multi-range must return 200, got %d", r.StatusCode)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("body=%q want full", body)
	}
}

func TestS3VersioningSuspendedRestituted(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "vsusp"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	get := func() string {
		resp := s.do(t, http.MethodGet, "/vsusp", "versioning=", nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out VersioningConfigurationXML
		xml.Unmarshal(body, &out)
		return out.Status
	}

	if st := get(); st != "" {
		t.Fatalf("fresh bucket status=%q want empty", st)
	}

	resp := s.do(t, http.MethodPut, "/vsusp", "versioning=", []byte(`<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>`))
	resp.Body.Close()
	if st := get(); st != "Enabled" {
		t.Fatalf("status=%q want Enabled", st)
	}

	resp = s.do(t, http.MethodPut, "/vsusp", "versioning=", []byte(`<VersioningConfiguration><Status>Suspended</Status></VersioningConfiguration>`))
	resp.Body.Close()
	if st := get(); st != "Suspended" {
		t.Fatalf("status=%q want Suspended", st)
	}
}

func TestS3ListMultipartUploadsFields(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lmu"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	for _, k := range []string{"docs/a.bin", "docs/b.bin", "img/c.bin"} {
		resp := s.do(t, http.MethodPost, "/lmu/"+k, "uploads=", nil)
		resp.Body.Close()
	}

	resp := s.do(t, http.MethodGet, "/lmu", "uploads=&delimiter=/", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var out ListMultipartUploadsResult
	if err := xml.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	if out.MaxUploads != 1000 {
		t.Fatalf("MaxUploads=%d want 1000", out.MaxUploads)
	}
	if len(out.CommonPrefixes) != 2 {
		t.Fatalf("common prefixes=%d want 2 (docs/, img/): %+v", len(out.CommonPrefixes), out.CommonPrefixes)
	}
	if len(out.Uploads) != 0 {
		t.Fatalf("with delimiter all roll into prefixes, got %d uploads", len(out.Uploads))
	}

	resp = s.do(t, http.MethodGet, "/lmu", "uploads=&prefix=docs/", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	out = ListMultipartUploadsResult{}
	xml.Unmarshal(body, &out)
	if len(out.Uploads) != 2 {
		t.Fatalf("prefix docs/ uploads=%d want 2", len(out.Uploads))
	}

	resp = s.do(t, http.MethodGet, "/lmu", "uploads=&max-uploads=1", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	out = ListMultipartUploadsResult{}
	xml.Unmarshal(body, &out)
	if !out.IsTruncated || len(out.Uploads) != 1 {
		t.Fatalf("max-uploads=1 truncated=%v uploads=%d", out.IsTruncated, len(out.Uploads))
	}
	if out.NextKeyMarker == "" {
		t.Fatal("truncated missing NextKeyMarker")
	}
}

func TestS3HeadBucketRegion(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "regbucket"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodHead, "/regbucket", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head status=%d", resp.StatusCode)
	}
	if resp.Header.Get("x-amz-bucket-region") == "" {
		t.Fatal("missing x-amz-bucket-region")
	}
}

func TestS3ResponseHeaderOverrides(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "respov"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/respov/o.txt", "", []byte("data"))
	resp.Body.Close()

	q := "response-content-type=application/json&response-content-disposition=attachment%3B%20filename%3Dx.json&response-cache-control=no-cache"
	resp = s.do(t, http.MethodGet, "/respov/o.txt", q, nil)
	resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type=%q want application/json", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=x.json" {
		t.Fatalf("Content-Disposition=%q", cd)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control=%q", cc)
	}
}

func TestS3GetObjectByPartNumber(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "partget"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/partget/f.bin", "uploads=", nil)
	var init InitiateMultipartUploadResult
	xml.NewDecoder(resp.Body).Decode(&init)
	resp.Body.Close()

	part1 := bytes.Repeat([]byte("A"), 20)
	part2 := bytes.Repeat([]byte("B"), 12)
	var etags []string
	for i, p := range [][]byte{part1, part2} {
		q := "uploadId=" + init.UploadID + "&partNumber=" + strconv.Itoa(i+1)
		r := s.do(t, http.MethodPut, "/partget/f.bin", q, p)
		etags = append(etags, r.Header.Get("ETag"))
		r.Body.Close()
	}

	cb := `<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>` + etags[0] + `</ETag></Part><Part><PartNumber>2</PartNumber><ETag>` + etags[1] + `</ETag></Part></CompleteMultipartUpload>`
	resp = s.do(t, http.MethodPost, "/partget/f.bin", "uploadId="+init.UploadID, []byte(cb))
	resp.Body.Close()

	req := s.sign(t, http.MethodGet, "/partget/f.bin", "partNumber=2", nil)
	r, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	if r.StatusCode != http.StatusPartialContent {
		t.Fatalf("partNumber=2 status=%d want 206", r.StatusCode)
	}
	if !bytes.Equal(body, part2) {
		t.Fatalf("part2 body=%q want %q", body, part2)
	}
	if pc := r.Header.Get("x-amz-mp-parts-count"); pc != "2" {
		t.Fatalf("parts-count=%q want 2", pc)
	}
	if cr := r.Header.Get("Content-Range"); cr != "bytes 20-31/32" {
		t.Fatalf("content-range=%q want bytes 20-31/32", cr)
	}

	req = s.sign(t, http.MethodGet, "/partget/f.bin", "partNumber=5", nil)
	r, _ = s.client.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("partNumber=5 status=%d want 416", r.StatusCode)
	}
}

func TestS3GetPartNumberSinglePart(t *testing.T) {
	s := newS3TestServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "singlepart"}); err != nil {
		t.Fatalf("bucket: %v", err)
	}
	resp := s.do(t, http.MethodPut, "/singlepart/o.txt", "", []byte("hello"))
	resp.Body.Close()

	req := s.sign(t, http.MethodGet, "/singlepart/o.txt", "partNumber=1", nil)
	r, _ := s.client.Do(req)
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("partNumber=1 single-part status=%d body=%q", r.StatusCode, body)
	}
	if pc := r.Header.Get("x-amz-mp-parts-count"); pc != "1" {
		t.Fatalf("parts-count=%q want 1", pc)
	}

	req = s.sign(t, http.MethodGet, "/singlepart/o.txt", "partNumber=2", nil)
	r, _ = s.client.Do(req)
	r.Body.Close()
	if r.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("partNumber=2 single-part status=%d want 416", r.StatusCode)
	}
}
