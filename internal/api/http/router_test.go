package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/testutil"
	"github.com/gofiber/fiber/v2"
)

type testServer struct {
	app    *fiber.App
	client *http.Client
	url    string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	testutil.SetupServer(t)
	app := Router()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- app.Listener(ln)
	}()

	t.Cleanup(func() {
		_ = app.ShutdownWithTimeout(5 * time.Second)
		<-serveErr
	})

	return &testServer{
		app:    app,
		client: &http.Client{Timeout: 10 * time.Second},
		url:    "http://" + ln.Addr().String(),
	}
}

func (s *testServer) do(t *testing.T, method, path string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func (s *testServer) createBucket(t *testing.T, name string) {
	t.Helper()
	resp := s.do(t, http.MethodPut, "/admin/buckets/"+name, nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create bucket %s: status=%d body=%s", name, resp.StatusCode, string(b))
	}
}

func (s *testServer) createToken(t *testing.T, bucket string, perms []string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"permissions": perms})
	resp := s.do(t, http.MethodPost, "/admin/buckets/"+bucket+"/tokens", bytes.NewReader(body), "application/json")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("createToken status=%d body=%s", resp.StatusCode, string(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out); err != nil {
		t.Fatalf("token unmarshal: %v", err)
	}
	return out.Token
}

func (s *testServer) doAuth(t *testing.T, method, path, token string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()
	return b
}

func TestStatus(t *testing.T) {
	s := newTestServer(t)
	resp := s.do(t, http.MethodGet, "/status", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
}

func TestAdminBucketCRUD(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		setup      func(s *testServer)
		wantStatus int
	}{
		{
			name:       "create new bucket",
			method:     http.MethodPut,
			path:       "/admin/buckets/mybucket",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "create reserved bucket admin",
			method:     http.MethodPut,
			path:       "/admin/buckets/admin",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create reserved bucket status",
			method:     http.MethodPut,
			path:       "/admin/buckets/status",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create invalid bucket name uppercase",
			method:     http.MethodPut,
			path:       "/admin/buckets/MyBucket",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create bucket name too short",
			method:     http.MethodPut,
			path:       "/admin/buckets/ab",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "create duplicate bucket",
			setup: func(s *testServer) {
				s.createBucket(t, "duplicate")
			},
			method:     http.MethodPut,
			path:       "/admin/buckets/duplicate",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "get existing bucket",
			setup: func(s *testServer) {
				s.createBucket(t, "getme")
			},
			method:     http.MethodGet,
			path:       "/admin/buckets/getme",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get missing bucket",
			method:     http.MethodGet,
			path:       "/admin/buckets/nopebucket",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete empty bucket",
			setup: func(s *testServer) {
				s.createBucket(t, "todelete")
			},
			method:     http.MethodDelete,
			path:       "/admin/buckets/todelete",
			wantStatus: http.StatusOK,
		},
		{
			name:       "delete missing bucket",
			method:     http.MethodDelete,
			path:       "/admin/buckets/ghostbucket",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			if tt.setup != nil {
				tt.setup(s)
			}
			resp := s.do(t, tt.method, tt.path, nil, "")
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, tt.wantStatus, string(b))
			}
		})
	}
}

func TestListBucketsEmpty(t *testing.T) {
	s := newTestServer(t)

	resp := s.do(t, http.MethodGet, "/admin/buckets", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=200", resp.StatusCode)
	}
	var out struct {
		Buckets []any `json:"buckets"`
		Count   int   `json:"count"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 0 {
		t.Fatalf("expected 0 buckets, got %d", out.Count)
	}
}

func TestListBucketsPopulated(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "alpha")
	s.createBucket(t, "betacat")

	resp := s.do(t, http.MethodGet, "/admin/buckets", nil, "")
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(readBody(t, resp), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 2 {
		t.Fatalf("expected 2 buckets, got %d", out.Count)
	}
}

func TestObjectRoundtrip(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "datas")
	tok := s.createToken(t, "datas", []string{"read", "write", "delete"})

	payload := []byte("hello hangar — content addressed storage test payload")

	resp := s.doAuth(t, http.MethodPut, "/datas/folder/file.txt", tok, bytes.NewReader(payload), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT status=%d body=%s headers=%v", resp.StatusCode, string(b), resp.Header)
	}
	var putOut struct {
		Key        string `json:"key"`
		Filename   string `json:"filename"`
		ETag       string `json:"etag"`
		Size       int64  `json:"size"`
		ObjectHash string `json:"object_hash"`
	}
	if err := json.Unmarshal(readBody(t, resp), &putOut); err != nil {
		t.Fatalf("PUT unmarshal: %v", err)
	}
	if putOut.Key != "folder/file.txt" {
		t.Errorf("key: got %q want %q", putOut.Key, "folder/file.txt")
	}
	if putOut.Filename != "file.txt" {
		t.Errorf("filename: got %q want %q", putOut.Filename, "file.txt")
	}
	if putOut.Size != int64(len(payload)) {
		t.Errorf("size: got %d want %d", putOut.Size, len(payload))
	}
	if !strings.HasPrefix(putOut.ETag, `"`) || !strings.HasSuffix(putOut.ETag, `"`) {
		t.Errorf("etag not quoted: %s", putOut.ETag)
	}

	resp = s.doAuth(t, http.MethodGet, "/datas/folder/file.txt", tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d", resp.StatusCode)
	}
	got := readBody(t, resp)
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
}

func TestObjectDownloadMissing(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "emptybucket")
	tok := s.createToken(t, "emptybucket", []string{"read"})

	resp := s.doAuth(t, http.MethodGet, "/emptybucket/nope.txt", tok, nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want=404", resp.StatusCode)
	}
}

func TestObjectUploadMissingBucket(t *testing.T) {
	s := newTestServer(t)

	resp := s.do(t, http.MethodPut, "/nobucket/file.txt", bytes.NewReader([]byte("x")), "application/octet-stream")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want=401", resp.StatusCode)
	}
}

func TestObjectDelete(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "delbucket")
	tok := s.createToken(t, "delbucket", []string{"read", "write", "delete"})

	resp := s.doAuth(t, http.MethodPut, "/delbucket/x.bin", tok, bytes.NewReader([]byte("payload")), "application/octet-stream")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = s.doAuth(t, http.MethodDelete, "/delbucket/x.bin", tok, nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d want=204", resp.StatusCode)
	}

	resp = s.doAuth(t, http.MethodDelete, "/delbucket/x.bin", tok, nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete status=%d want=404", resp.StatusCode)
	}

	resp = s.doAuth(t, http.MethodGet, "/delbucket/x.bin", tok, nil, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status=%d want=404", resp.StatusCode)
	}
}

func TestObjectPathTraversalRejected(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "safebucket")
	tok := s.createToken(t, "safebucket", []string{"write"})

	req, err := http.NewRequest(http.MethodPut, s.url+"/safebucket/../etc/passwd", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.URL.Opaque = "/safebucket/../etc/passwd"
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		t.Fatalf("path traversal accepted: status=%d", resp.StatusCode)
	}
}

func TestListObjects(t *testing.T) {
	s := newTestServer(t)
	s.createBucket(t, "lsbucket")
	tok := s.createToken(t, "lsbucket", []string{"read", "write"})

	for _, k := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		resp := s.doAuth(t, http.MethodPut, "/lsbucket/"+k, tok, bytes.NewReader([]byte("data-"+k)), "application/octet-stream")
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("upload %s status=%d body=%s", k, resp.StatusCode, string(b))
		}
		resp.Body.Close()
	}

	resp := s.doAuth(t, http.MethodGet, "/lsbucket", tok, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, k := range []string{"a.txt", "b.txt", "sub/c.txt"} {
		if !bytes.Contains(body, []byte(k)) {
			t.Errorf("missing %s in list output: %s", k, body)
		}
	}
}

func TestListObjectsBucketMissing(t *testing.T) {
	s := newTestServer(t)

	resp := s.do(t, http.MethodGet, "/ghostbucket", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want=401", resp.StatusCode)
	}
}
