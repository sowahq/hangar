package object

import (
	"bytes"
	"io"
	"testing"

	"github.com/anhostfr/hangar/internal/service/sse"
	"github.com/anhostfr/hangar/internal/testutil"
)

func TestSSES3ReadAfterRotation(t *testing.T) {
	testutil.SetupServer(t)
	setMaster(t)

	body := randBytes(t, 1024*3+11)

	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj1", Body: bytes.NewReader(body), SSE: &SSERequest{Algorithm: SSEAlgoS3}}); err != nil {
		t.Fatalf("put obj1: %v", err)
	}

	newID, err := sse.Rotate()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newID == "" {
		t.Fatal("empty new key id")
	}

	body2 := randBytes(t, 800)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj2", Body: bytes.NewReader(body2), SSE: &SSERequest{Algorithm: SSEAlgoS3}}); err != nil {
		t.Fatalf("put obj2: %v", err)
	}

	resp, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj1"})
	if err != nil {
		t.Fatalf("get obj1: %v", err)
	}
	got, _ := io.ReadAll(resp.Reader)
	if !bytes.Equal(got, body) {
		t.Fatal("obj1 mismatch (old key unreadable after rotation)")
	}

	resp2, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj2"})
	if err != nil {
		t.Fatalf("get obj2: %v", err)
	}
	got2, _ := io.ReadAll(resp2.Reader)
	if !bytes.Equal(got2, body2) {
		t.Fatal("obj2 mismatch")
	}
}
