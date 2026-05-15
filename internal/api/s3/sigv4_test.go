package s3

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseAuthorization(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
		want    *AuthHeader
	}{
		{
			name: "valid",
			value: "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host;x-amz-date, Signature=abc123",
			want: &AuthHeader{
				AccessKeyID:   "AKIDEXAMPLE",
				Date:          "20150830",
				Region:        "us-east-1",
				Service:       "s3",
				SignedHeaders: []string{"host", "x-amz-date"},
				Signature:     "abc123",
			},
		},
		{
			name:    "missing algorithm prefix",
			value:   "Credential=AK/20150830/us-east-1/s3/aws4_request, SignedHeaders=host, Signature=x",
			wantErr: true,
		},
		{
			name: "bad scope length",
			value: "AWS4-HMAC-SHA256 Credential=AK/20150830/us-east-1/s3, " +
				"SignedHeaders=host, Signature=x",
			wantErr: true,
		},
		{
			name: "bad terminator",
			value: "AWS4-HMAC-SHA256 Credential=AK/20150830/us-east-1/s3/wrong, " +
				"SignedHeaders=host, Signature=x",
			wantErr: true,
		},
		{
			name: "missing signature",
			value: "AWS4-HMAC-SHA256 Credential=AK/20150830/us-east-1/s3/aws4_request, " +
				"SignedHeaders=host",
			wantErr: true,
		},
		{
			name:    "empty",
			value:   "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAuthorization(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.AccessKeyID != tc.want.AccessKeyID ||
				got.Date != tc.want.Date ||
				got.Region != tc.want.Region ||
				got.Service != tc.want.Service ||
				got.Signature != tc.want.Signature ||
				strings.Join(got.SignedHeaders, ";") != strings.Join(tc.want.SignedHeaders, ";") {
				t.Fatalf("mismatch: got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestCanonicalURI(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"", "/"},
		{"/", "/"},
		{"/foo/bar", "/foo/bar"},
		{"/foo bar", "/foo%20bar"},
		{"/documents%20and%20settings/", "/documents%20and%20settings/"},
		{"/ütf8", "/%C3%BCtf8"},
	}
	for _, tc := range cases {
		if got := canonicalURI(tc.in); got != tc.out {
			t.Errorf("canonicalURI(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestCanonicalQuery(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{"b=2&a=1", "a=1&b=2"},
		{"a=&b=2", "a=&b=2"},
		{"foo", "foo="},
		{"a=1&a=2", "a=1&a=2"},
		{"prefix=foo%20bar", "prefix=foo%20bar"},
		{"list-type=2&prefix=", "list-type=2&prefix="},
	}
	for _, tc := range cases {
		if got := canonicalQuery(tc.in); got != tc.out {
			t.Errorf("canonicalQuery(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestCollapseWhitespace(t *testing.T) {
	cases := []struct{ in, out string }{
		{"  a  b  c ", "a b c"},
		{"a\tb", "a b"},
		{"foo", "foo"},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := collapseWhitespace(tc.in); got != tc.out {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

// AWS SigV4 published test vector: get-vanilla
// Source: https://docs.aws.amazon.com/general/latest/gr/sigv4-signed-request-examples.html
// Note: AWS suite uses service="service" not "s3". Tests use lower-level fns directly.
func TestAWSGetVanillaVector(t *testing.T) {
	const (
		secret    = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		date      = "20150830"
		region    = "us-east-1"
		service   = "service"
		amzDate   = "20150830T123600Z"
		host      = "example.amazonaws.com"
		wantSig   = "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	)

	r := &Request{
		Method:   "GET",
		Path:     "/",
		RawQuery: "",
		Headers: http.Header{
			"Host":         {host},
			"X-Amz-Date":   {amzDate},
		},
	}
	cr, signed, err := CanonicalRequest(r, []string{"host", "x-amz-date"}, emptyStringSHA256)
	if err != nil {
		t.Fatalf("CanonicalRequest: %v", err)
	}
	if signed != "host;x-amz-date" {
		t.Fatalf("signed headers: %q", signed)
	}
	expectedCR := "GET\n/\n\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n" + emptyStringSHA256
	if cr != expectedCR {
		t.Fatalf("canonical request mismatch:\ngot:  %q\nwant: %q", cr, expectedCR)
	}

	sts := StringToSign(amzDate, date, region, service, sha256Hex(cr))
	expectedSTS := "AWS4-HMAC-SHA256\n20150830T123600Z\n20150830/us-east-1/service/aws4_request\n" + sha256Hex(cr)
	if sts != expectedSTS {
		t.Fatalf("STS mismatch:\ngot:  %q\nwant: %q", sts, expectedSTS)
	}

	key := DeriveSigningKey(secret, date, region, service)
	got := Sign(sts, key)
	if got != wantSig {
		t.Fatalf("signature: got %q want %q", got, wantSig)
	}
}

// AWS SigV4 published test vector: get-vanilla-query-order-key-case
func TestAWSGetVanillaQueryVector(t *testing.T) {
	const (
		secret  = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		date    = "20150830"
		region  = "us-east-1"
		service = "service"
		amzDate = "20150830T123600Z"
		host    = "example.amazonaws.com"
	)
	r := &Request{
		Method:   "GET",
		Path:     "/",
		RawQuery: "Param2=value2&Param1=value1",
		Headers: http.Header{
			"Host":       {host},
			"X-Amz-Date": {amzDate},
		},
	}
	cr, _, err := CanonicalRequest(r, []string{"host", "x-amz-date"}, emptyStringSHA256)
	if err != nil {
		t.Fatalf("CanonicalRequest: %v", err)
	}
	expectedCR := "GET\n/\nParam1=value1&Param2=value2\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n" + emptyStringSHA256
	if cr != expectedCR {
		t.Fatalf("canonical request mismatch:\ngot:  %q\nwant: %q", cr, expectedCR)
	}
	sts := StringToSign(amzDate, date, region, service, sha256Hex(cr))
	key := DeriveSigningKey(secret, date, region, service)
	_ = Sign(sts, key)
}

func TestVerifyRoundTrip(t *testing.T) {
	const (
		accessKey = "AKIAIOSFODNN7EXAMPLE"
		secret    = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		date      = "20260515"
		amzDate   = "20260515T120000Z"
		region    = "us-east-1"
		host      = "s3.local:9000"
		payload   = "hello world"
	)
	payloadHash := sha256Hex(payload)

	r := &Request{
		Method:   "PUT",
		Path:     "/mybucket/some-key.txt",
		RawQuery: "x-id=PutObject",
		Headers: http.Header{
			"Host":                 {host},
			"X-Amz-Date":           {amzDate},
			"X-Amz-Content-Sha256": {payloadHash},
		},
	}
	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	cr, signedJoined, err := CanonicalRequest(r, signed, payloadHash)
	if err != nil {
		t.Fatalf("CanonicalRequest: %v", err)
	}
	sts := StringToSign(amzDate, date, region, "s3", sha256Hex(cr))
	key := DeriveSigningKey(secret, date, region, "s3")
	sig := Sign(sts, key)

	auth := "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + date + "/" + region + "/s3/aws4_request, " +
		"SignedHeaders=" + signedJoined + ", Signature=" + sig
	r.Headers.Set(headerAuthorization, auth)

	now, _ := time.Parse("20060102T150405Z", amzDate)
	lookup := func(ak string) (string, error) {
		if ak != accessKey {
			return "", errors.New("not found")
		}
		return secret, nil
	}
	ah, err := Verify(r, lookup, now)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ah.AccessKeyID != accessKey {
		t.Fatalf("ak: %q", ah.AccessKeyID)
	}

	// Tamper signature.
	r.Headers.Set(headerAuthorization, auth[:len(auth)-4]+"dead")
	if _, err := Verify(r, lookup, now); !errors.Is(err, ErrSigV4BadSignature) {
		t.Fatalf("expected ErrSigV4BadSignature, got %v", err)
	}

	// Restore + unknown key.
	r.Headers.Set(headerAuthorization, auth)
	badLookup := func(string) (string, error) { return "", errors.New("nope") }
	if _, err := Verify(r, badLookup, now); !errors.Is(err, ErrSigV4UnknownKey) {
		t.Fatalf("expected ErrSigV4UnknownKey, got %v", err)
	}

	// Clock skew.
	skewed := now.Add(20 * time.Minute)
	if _, err := Verify(r, lookup, skewed); !errors.Is(err, ErrSigV4ClockSkew) {
		t.Fatalf("expected ErrSigV4ClockSkew, got %v", err)
	}

	// Chunked unsupported.
	r2 := *r
	r2.Headers = r.Headers.Clone()
	r2.Headers.Set(headerContentSHA256, PayloadStreaming)
	if _, err := Verify(&r2, lookup, now); !errors.Is(err, ErrSigV4ChunkedUnsupported) {
		t.Fatalf("expected ErrSigV4ChunkedUnsupported, got %v", err)
	}

	// Missing payload hash.
	r3 := *r
	r3.Headers = r.Headers.Clone()
	r3.Headers.Del(headerContentSHA256)
	if _, err := Verify(&r3, lookup, now); !errors.Is(err, ErrSigV4MissingPayloadHash) {
		t.Fatalf("expected ErrSigV4MissingPayloadHash, got %v", err)
	}
}

func TestVerifyWrongService(t *testing.T) {
	const accessKey = "AK"
	r := &Request{
		Method: "GET",
		Path:   "/",
		Headers: http.Header{
			"Host":                 {"x"},
			"X-Amz-Date":           {"20260515T120000Z"},
			"X-Amz-Content-Sha256": {emptyStringSHA256},
			headerAuthorization: {
				"AWS4-HMAC-SHA256 Credential=" + accessKey + "/20260515/us-east-1/iam/aws4_request, " +
					"SignedHeaders=host, Signature=deadbeef",
			},
		},
	}
	now, _ := time.Parse("20060102T150405Z", "20260515T120000Z")
	_, err := Verify(r, func(string) (string, error) { return "x", nil }, now)
	if !errors.Is(err, ErrSigV4Malformed) {
		t.Fatalf("expected ErrSigV4Malformed (wrong service), got %v", err)
	}
}

func TestRequestFromHTTP(t *testing.T) {
	req, err := http.NewRequest("GET", "http://example.com/foo%20bar?b=2&a=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Date", "20260515T120000Z")
	r := RequestFromHTTP(req)
	if r.Method != "GET" || r.Path != "/foo%20bar" || r.RawQuery != "b=2&a=1" {
		t.Fatalf("got %+v", r)
	}
	if r.Headers.Get("Host") != "example.com" {
		t.Fatalf("host not propagated: %q", r.Headers.Get("Host"))
	}
}
