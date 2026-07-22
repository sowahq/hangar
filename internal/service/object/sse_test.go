package object

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/testutil"
)

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

func customerSSE(t *testing.T, key []byte) *SSERequest {
	t.Helper()
	sum := md5.Sum(key)
	return &SSERequest{
		Algorithm:      SSEAlgoC,
		CustomerKey:    key,
		CustomerKeyMD5: base64.StdEncoding.EncodeToString(sum[:]),
	}
}

func setMaster(t *testing.T) {
	t.Helper()
	master := randBytes(t, 32)
	config.SetMasterKeyForTest(master)
	t.Cleanup(func() { config.SetMasterKeyForTest(nil) })
}

func TestPutGetSSERoundtrip(t *testing.T) {
	custKey := randBytes(t, 32)

	tests := []struct {
		name   string
		master bool
		put    *SSERequest
		get    *SSERequest
		size   int
	}{
		{"none small", false, nil, nil, 512},
		{"none multi-chunk", false, nil, nil, 1024*3 + 17},
		{"sse-s3 small", true, &SSERequest{Algorithm: SSEAlgoS3}, nil, 512},
		{"sse-s3 multi-chunk", true, &SSERequest{Algorithm: SSEAlgoS3}, nil, 1024*3 + 17},
		{"sse-c small", false, customerSSE(t, custKey), customerSSE(t, custKey), 512},
		{"sse-c multi-chunk", false, customerSSE(t, custKey), customerSSE(t, custKey), 1024*3 + 17},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupServer(t)
			if tc.master {
				setMaster(t)
			}

			body := randBytes(t, tc.size)
			putResp, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body), SSE: tc.put})
			if err != nil {
				t.Fatalf("PutObject: %v", err)
			}
			wantAlgo := SSEAlgoNone
			if tc.put != nil {
				wantAlgo = tc.put.Algorithm
			}
			if putResp.SSEAlgorithm != wantAlgo {
				t.Fatalf("put algo: got=%q want=%q", putResp.SSEAlgorithm, wantAlgo)
			}

			resp, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj", SSE: tc.get})
			if err != nil {
				t.Fatalf("GetObject: %v", err)
			}
			got, err := io.ReadAll(resp.Reader)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("roundtrip mismatch: got %d bytes want %d", len(got), len(body))
			}
		})
	}
}

func TestPutSSES3MasterKeyMissing(t *testing.T) {
	testutil.SetupServer(t)
	config.SetMasterKeyForTest(nil)

	body := randBytes(t, 32)
	_, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body), SSE: &SSERequest{Algorithm: SSEAlgoS3}})
	if !errors.Is(err, ErrSSEMasterKeyMissing) {
		t.Fatalf("expected ErrSSEMasterKeyMissing, got %v", err)
	}
}

func TestGetSSECErrors(t *testing.T) {
	custKey := randBytes(t, 32)
	otherKey := randBytes(t, 32)

	tests := []struct {
		name    string
		get     *SSERequest
		wantErr error
	}{
		{"missing customer headers", nil, ErrSSECustomerKeyRequired},
		{"wrong customer key (md5 mismatch)", customerSSE(t, otherKey), ErrSSECustomerKeyMD5Mismatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupServer(t)

			body := randBytes(t, 256)
			if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body), SSE: customerSSE(t, custKey)}); err != nil {
				t.Fatalf("PutObject: %v", err)
			}

			_, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj", SSE: tc.get})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got err=%v want=%v", err, tc.wantErr)
			}
		})
	}
}

func TestGetSSECustomerOnUnencrypted(t *testing.T) {
	testutil.SetupServer(t)

	body := randBytes(t, 64)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "plain", Body: bytes.NewReader(body)}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	_, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "plain", SSE: customerSSE(t, randBytes(t, 32))})
	if !errors.Is(err, ErrSSECustomerOnUnencrypted) {
		t.Fatalf("expected ErrSSECustomerOnUnencrypted, got %v", err)
	}
}

func TestGetSSECustomerOnS3Object(t *testing.T) {
	testutil.SetupServer(t)
	setMaster(t)

	body := randBytes(t, 64)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "k", Body: bytes.NewReader(body), SSE: &SSERequest{Algorithm: SSEAlgoS3}}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	_, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "k", SSE: customerSSE(t, randBytes(t, 32))})
	if !errors.Is(err, ErrSSECustomerForS3Object) {
		t.Fatalf("expected ErrSSECustomerForS3Object, got %v", err)
	}
}

func TestSSES3CiphertextOnDisk(t *testing.T) {
	testutil.SetupServer(t)
	setMaster(t)

	body := bytes.Repeat([]byte("PLAINTEXT-MARKER-"), 64)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "k", Body: bytes.NewReader(body), SSE: &SSERequest{Algorithm: SSEAlgoS3}}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	found := false
	err := filepath.Walk(config.ChunksPath(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, rErr := os.ReadFile(path)
		if rErr != nil {
			return rErr
		}
		found = true
		if bytes.Contains(data, []byte("PLAINTEXT-MARKER")) {
			t.Fatalf("plaintext marker found in chunk file %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !found {
		t.Fatal("no chunk files written")
	}
}

func TestCopyObjectSSEMatrix(t *testing.T) {
	keyA := randBytes(t, 32)
	keyB := randBytes(t, 32)

	tests := []struct {
		name   string
		srcSSE *SSERequest
		dstSSE *SSERequest
		srcGet *SSERequest
		dstGet *SSERequest
		master bool
	}{
		{"plain to plain (fast)", nil, nil, nil, nil, false},
		{"plain to sse-s3", nil, &SSERequest{Algorithm: SSEAlgoS3}, nil, nil, true},
		{"sse-c to plain", customerSSE(t, keyA), nil, customerSSE(t, keyA), nil, false},
		{"sse-c to sse-c (rotate)", customerSSE(t, keyA), customerSSE(t, keyB), customerSSE(t, keyA), customerSSE(t, keyB), false},
		{"sse-s3 to sse-c", &SSERequest{Algorithm: SSEAlgoS3}, customerSSE(t, keyA), nil, customerSSE(t, keyA), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupServer(t)
			if tc.master {
				setMaster(t)
			}
			if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "src"}); err != nil {
				t.Fatalf("src bucket: %v", err)
			}
			if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "dst"}); err != nil {
				t.Fatalf("dst bucket: %v", err)
			}

			body := randBytes(t, 1024*2+99)
			if _, err := PutObject(&PutObjectRequest{Bucket: "src", Key: "orig", Body: bytes.NewReader(body), SSE: tc.srcSSE}); err != nil {
				t.Fatalf("seed put: %v", err)
			}

			if _, err := CopyObject(&CopyObjectRequest{
				SrcBucket: "src", SrcKey: "orig",
				DstBucket: "dst", DstKey: "copy",
				SrcSSE: tc.srcGet, DstSSE: tc.dstSSE,
			}); err != nil {
				t.Fatalf("CopyObject: %v", err)
			}

			resp, err := GetObject(&GetObjectRequest{Bucket: "dst", Key: "copy", SSE: tc.dstGet})
			if err != nil {
				t.Fatalf("GetObject dst: %v", err)
			}
			got, err := io.ReadAll(resp.Reader)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Fatalf("copy mismatch: got %d want %d", len(got), len(body))
			}
		})
	}
}

func TestMultipartSSES3Roundtrip(t *testing.T) {
	testutil.SetupServer(t)
	setMaster(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "mpu"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}

	init, err := InitiateMultipart(&InitiateMultipartRequest{
		Bucket: "mpu", Key: "big", SSE: &SSERequest{Algorithm: SSEAlgoS3},
	})
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	part1 := bytes.Repeat([]byte("A"), 1024*2)
	part2 := bytes.Repeat([]byte("B"), 1024*2+50)

	for i, body := range [][]byte{part1, part2} {
		if _, err := UploadPart(&UploadPartRequest{
			Bucket: "mpu", Key: "big", UploadID: init.UploadID,
			PartNumber: i + 1, Body: bytes.NewReader(body),
		}); err != nil {
			t.Fatalf("UploadPart %d: %v", i+1, err)
		}
	}

	if _, err := CompleteMultipart(&CompleteMultipartRequest{
		Bucket: "mpu", Key: "big", UploadID: init.UploadID, Parts: []int{1, 2},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	resp, err := GetObject(&GetObjectRequest{Bucket: "mpu", Key: "big"})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	got, err := io.ReadAll(resp.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := append(append([]byte{}, part1...), part2...)
	if !bytes.Equal(got, want) {
		t.Fatalf("multipart sse-s3 mismatch: got %d want %d", len(got), len(want))
	}
}

func TestMultipartSSECRoundtrip(t *testing.T) {
	testutil.SetupServer(t)
	if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "mpuc"}); err != nil {
		t.Fatalf("seed bucket: %v", err)
	}

	custKey := randBytes(t, 32)
	sse := customerSSE(t, custKey)

	init, err := InitiateMultipart(&InitiateMultipartRequest{
		Bucket: "mpuc", Key: "big", SSE: sse,
	})
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	part1 := bytes.Repeat([]byte("X"), 1024*2)
	part2 := bytes.Repeat([]byte("Y"), 700)

	for i, body := range [][]byte{part1, part2} {
		if _, err := UploadPart(&UploadPartRequest{
			Bucket: "mpuc", Key: "big", UploadID: init.UploadID,
			PartNumber: i + 1, Body: bytes.NewReader(body), SSE: sse,
		}); err != nil {
			t.Fatalf("UploadPart %d: %v", i+1, err)
		}
	}

	if _, err := CompleteMultipart(&CompleteMultipartRequest{
		Bucket: "mpuc", Key: "big", UploadID: init.UploadID, Parts: []int{1, 2},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	resp, err := GetObject(&GetObjectRequest{Bucket: "mpuc", Key: "big", SSE: sse})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	got, err := io.ReadAll(resp.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := append(append([]byte{}, part1...), part2...)
	if !bytes.Equal(got, want) {
		t.Fatalf("multipart sse-c mismatch: got %d want %d", len(got), len(want))
	}

	if _, err := GetObject(&GetObjectRequest{Bucket: "mpuc", Key: "big"}); !errors.Is(err, ErrSSECustomerKeyRequired) {
		t.Fatalf("GET without key: got %v want ErrSSECustomerKeyRequired", err)
	}
}

func TestParseCustomerKey(t *testing.T) {
	good := randBytes(t, 32)
	goodB64 := base64.StdEncoding.EncodeToString(good)
	sum := md5.Sum(good)
	goodMD5 := base64.StdEncoding.EncodeToString(sum[:])

	tests := []struct {
		name    string
		key     string
		md5     string
		wantErr error
	}{
		{"ok", goodB64, goodMD5, nil},
		{"missing key", "", goodMD5, ErrSSECustomerKeyRequired},
		{"missing md5", goodB64, "", ErrSSECustomerKeyRequired},
		{"bad base64", "!!!not-base64!!!", goodMD5, ErrSSECustomerKeyInvalid},
		{"wrong length", base64.StdEncoding.EncodeToString(randBytes(t, 16)), goodMD5, ErrSSECustomerKeyInvalid},
		{"md5 mismatch", goodB64, strings.Repeat("A", 24), ErrSSECustomerKeyMD5Mismatch},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseCustomerKey(tc.key, tc.md5)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}
