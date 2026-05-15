package s3

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	signingAlgorithm = "AWS4-HMAC-SHA256"
	scopeTerminator  = "aws4_request"
	scopeService     = "s3"
	maxClockSkew     = 15 * time.Minute

	headerAuthorization = "Authorization"
	headerAmzDate       = "X-Amz-Date"
	headerDate          = "Date"
	headerContentSHA256 = "X-Amz-Content-Sha256"
	headerHost          = "Host"

	PayloadUnsigned    = "UNSIGNED-PAYLOAD"
	PayloadStreaming   = "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"
	emptyStringSHA256  = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var (
	ErrSigV4Malformed         = errors.New("sigv4: malformed authorization header")
	ErrSigV4BadSignature      = errors.New("sigv4: signature mismatch")
	ErrSigV4UnknownKey        = errors.New("sigv4: unknown access key id")
	ErrSigV4ClockSkew         = errors.New("sigv4: request time too skewed")
	ErrSigV4MissingDate       = errors.New("sigv4: missing X-Amz-Date or Date header")
	ErrSigV4MissingPayloadHash = errors.New("sigv4: missing X-Amz-Content-Sha256 header")
	ErrSigV4Expired           = errors.New("sigv4: presigned URL expired")
)

type AuthHeader struct {
	AccessKeyID   string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
	Streaming     bool
	AmzDate       string
	SigningKey    []byte
}

type Request struct {
	Method   string
	Path     string
	RawQuery string
	Headers  http.Header
}

func RequestFromHTTP(r *http.Request) *Request {
	h := r.Header.Clone()
	if h.Get(headerHost) == "" && r.Host != "" {
		h.Set(headerHost, r.Host)
	}
	return &Request{
		Method:   r.Method,
		Path:     r.URL.EscapedPath(),
		RawQuery: r.URL.RawQuery,
		Headers:  h,
	}
}

func ParseAuthorization(value string) (*AuthHeader, error) {
	if value == "" {
		return nil, ErrSigV4Malformed
	}
	if !strings.HasPrefix(value, signingAlgorithm+" ") {
		return nil, ErrSigV4Malformed
	}
	rest := strings.TrimSpace(value[len(signingAlgorithm):])
	parts := strings.Split(rest, ",")
	out := &AuthHeader{}
	seen := map[string]bool{}
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) != 2 {
			return nil, ErrSigV4Malformed
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		seen[k] = true
		switch k {
		case "Credential":
			scope := strings.Split(v, "/")
			if len(scope) != 5 {
				return nil, ErrSigV4Malformed
			}
			out.AccessKeyID = scope[0]
			out.Date = scope[1]
			out.Region = scope[2]
			out.Service = scope[3]
			if scope[4] != scopeTerminator {
				return nil, ErrSigV4Malformed
			}
		case "SignedHeaders":
			if v == "" {
				return nil, ErrSigV4Malformed
			}
			out.SignedHeaders = strings.Split(v, ";")
		case "Signature":
			out.Signature = v
		}
	}
	if !seen["Credential"] || !seen["SignedHeaders"] || !seen["Signature"] {
		return nil, ErrSigV4Malformed
	}
	if out.AccessKeyID == "" || out.Signature == "" || len(out.SignedHeaders) == 0 {
		return nil, ErrSigV4Malformed
	}
	return out, nil
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for i, s := range segments {
		decoded, err := url.PathUnescape(s)
		if err != nil {
			decoded = s
		}
		segments[i] = uriEncode(decoded, false)
	}
	return strings.Join(segments, "/")
}

func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	pairs := strings.Split(raw, "&")
	type kv struct{ k, v string }
	items := make([]kv, 0, len(pairs))
	for _, p := range pairs {
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		var k, v string
		if eq < 0 {
			k = p
			v = ""
		} else {
			k = p[:eq]
			v = p[eq+1:]
		}
		kd, err := url.QueryUnescape(k)
		if err != nil {
			kd = k
		}
		vd, err := url.QueryUnescape(v)
		if err != nil {
			vd = v
		}
		items = append(items, kv{uriEncode(kd, true), uriEncode(vd, true)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].k == items[j].k {
			return items[i].v < items[j].v
		}
		return items[i].k < items[j].k
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.k + "=" + it.v
	}
	return strings.Join(out, "&")
}

func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func canonicalHeaders(headers http.Header, signed []string) (string, string, error) {
	lowSigned := make([]string, len(signed))
	for i, s := range signed {
		lowSigned[i] = strings.ToLower(strings.TrimSpace(s))
	}
	sort.Strings(lowSigned)
	var b strings.Builder
	for _, name := range lowSigned {
		values := headerValuesCanon(headers, name)
		if values == "" {
			return "", "", fmt.Errorf("%w: signed header %q missing", ErrSigV4Malformed, name)
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(values)
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(lowSigned, ";"), nil
}

func headerValuesCanon(headers http.Header, name string) string {
	canonName := http.CanonicalHeaderKey(name)
	vals := headers.Values(canonName)
	if len(vals) == 0 && strings.EqualFold(name, "host") {
		if h := headers.Get(headerHost); h != "" {
			vals = []string{h}
		}
	}
	if len(vals) == 0 {
		return ""
	}
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = collapseWhitespace(v)
	}
	return strings.Join(out, ",")
}

func collapseWhitespace(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteByte(c)
	}
	return b.String()
}

func CanonicalRequest(r *Request, signedHeaders []string, payloadHash string) (string, string, error) {
	hdrs, signedJoined, err := canonicalHeaders(r.Headers, signedHeaders)
	if err != nil {
		return "", "", err
	}
	cr := r.Method + "\n" +
		canonicalURI(r.Path) + "\n" +
		canonicalQuery(r.RawQuery) + "\n" +
		hdrs + "\n" +
		signedJoined + "\n" +
		payloadHash
	return cr, signedJoined, nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func StringToSign(amzDate, date, region, service, canonicalReqHash string) string {
	scope := date + "/" + region + "/" + service + "/" + scopeTerminator
	return signingAlgorithm + "\n" + amzDate + "\n" + scope + "\n" + canonicalReqHash
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func DeriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte(scopeTerminator))
}

func Sign(stringToSign string, signingKey []byte) string {
	return hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
}

func parseAmzDate(s string) (time.Time, error) {
	if t, err := time.Parse("20060102T150405Z", s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC1123, s)
}

type SecretLookup func(accessKeyID string) (secret string, err error)

func Verify(r *Request, lookup SecretLookup, now time.Time) (*AuthHeader, error) {
	authVal := r.Headers.Get(headerAuthorization)
	if authVal == "" {
		if q, _ := url.ParseQuery(r.RawQuery); q.Get("X-Amz-Signature") != "" {
			return verifyPresigned(r, q, lookup, now)
		}
	}
	ah, err := ParseAuthorization(authVal)
	if err != nil {
		return nil, err
	}
	if ah.Service != scopeService {
		return nil, fmt.Errorf("%w: wrong service %q", ErrSigV4Malformed, ah.Service)
	}

	amzDate := r.Headers.Get(headerAmzDate)
	if amzDate == "" {
		amzDate = r.Headers.Get(headerDate)
	}
	if amzDate == "" {
		return nil, ErrSigV4MissingDate
	}
	t, err := parseAmzDate(amzDate)
	if err != nil {
		return nil, fmt.Errorf("%w: bad date %q", ErrSigV4Malformed, amzDate)
	}
	if !now.IsZero() {
		diff := now.Sub(t)
		if diff < 0 {
			diff = -diff
		}
		if diff > maxClockSkew {
			return nil, ErrSigV4ClockSkew
		}
	}

	payloadHash := r.Headers.Get(headerContentSHA256)
	if payloadHash == "" {
		return nil, ErrSigV4MissingPayloadHash
	}
	if payloadHash == PayloadStreaming {
		ah.Streaming = true
	}

	secret, err := lookup(ah.AccessKeyID)
	if err != nil {
		return nil, ErrSigV4UnknownKey
	}

	cr, _, err := CanonicalRequest(r, ah.SignedHeaders, payloadHash)
	if err != nil {
		return nil, err
	}
	sts := StringToSign(amzDate, ah.Date, ah.Region, ah.Service, sha256Hex(cr))
	key := DeriveSigningKey(secret, ah.Date, ah.Region, ah.Service)
	expected := Sign(sts, key)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(ah.Signature)) != 1 {
		return nil, ErrSigV4BadSignature
	}
	if ah.Streaming {
		ah.AmzDate = amzDate
		ah.SigningKey = key
	}
	return ah, nil
}

func verifyPresigned(r *Request, q url.Values, lookup SecretLookup, now time.Time) (*AuthHeader, error) {
	if q.Get("X-Amz-Algorithm") != signingAlgorithm {
		return nil, ErrSigV4Malformed
	}
	credential := q.Get("X-Amz-Credential")
	scope := strings.Split(credential, "/")
	if len(scope) != 5 || scope[4] != scopeTerminator {
		return nil, ErrSigV4Malformed
	}
	ah := &AuthHeader{
		AccessKeyID: scope[0],
		Date:        scope[1],
		Region:      scope[2],
		Service:     scope[3],
		Signature:   q.Get("X-Amz-Signature"),
	}
	if ah.Service != scopeService {
		return nil, fmt.Errorf("%w: wrong service %q", ErrSigV4Malformed, ah.Service)
	}
	signedHeadersRaw := q.Get("X-Amz-SignedHeaders")
	if signedHeadersRaw == "" {
		return nil, ErrSigV4Malformed
	}
	ah.SignedHeaders = strings.Split(signedHeadersRaw, ";")

	amzDate := q.Get("X-Amz-Date")
	if amzDate == "" {
		return nil, ErrSigV4MissingDate
	}
	t, err := parseAmzDate(amzDate)
	if err != nil {
		return nil, fmt.Errorf("%w: bad date %q", ErrSigV4Malformed, amzDate)
	}
	expiresStr := q.Get("X-Amz-Expires")
	if expiresStr == "" {
		return nil, ErrSigV4Malformed
	}
	expSec, err := time.ParseDuration(expiresStr + "s")
	if err != nil {
		return nil, fmt.Errorf("%w: bad expires %q", ErrSigV4Malformed, expiresStr)
	}
	if !now.IsZero() && now.After(t.Add(expSec)) {
		return nil, ErrSigV4Expired
	}
	if !now.IsZero() {
		diff := now.Sub(t)
		if diff < 0 {
			diff = -diff
		}
		if diff > maxClockSkew && now.Before(t) {
			return nil, ErrSigV4ClockSkew
		}
	}

	secret, err := lookup(ah.AccessKeyID)
	if err != nil {
		return nil, ErrSigV4UnknownKey
	}

	filteredRaw := stripQueryParam(r.RawQuery, "X-Amz-Signature")
	canonReq := &Request{
		Method:   r.Method,
		Path:     r.Path,
		RawQuery: filteredRaw,
		Headers:  r.Headers,
	}
	cr, _, err := CanonicalRequest(canonReq, ah.SignedHeaders, PayloadUnsigned)
	if err != nil {
		return nil, err
	}
	sts := StringToSign(amzDate, ah.Date, ah.Region, ah.Service, sha256Hex(cr))
	key := DeriveSigningKey(secret, ah.Date, ah.Region, ah.Service)
	expected := Sign(sts, key)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(ah.Signature)) != 1 {
		return nil, ErrSigV4BadSignature
	}
	return ah, nil
}

func stripQueryParam(raw, name string) string {
	if raw == "" {
		return ""
	}
	var out []string
	prefix := name + "="
	for _, p := range strings.Split(raw, "&") {
		if p == name || strings.HasPrefix(p, prefix) {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "&")
}
