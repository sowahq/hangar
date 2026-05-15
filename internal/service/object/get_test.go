package object

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/anhostfr/hangar/internal/testutil"
)

func TestGetObjectRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"single byte", 1},
		{"under chunk", 512},
		{"exact chunk", 1024},
		{"chunk plus one", 1025},
		{"multi chunk", 1024*3 + 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupServer(t)

			body := make([]byte, tc.size)
			if _, err := rand.Read(body); err != nil {
				t.Fatalf("rand.Read: %v", err)
			}

			if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body)}); err != nil {
				t.Fatalf("PutObject: %v", err)
			}

			resp, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj"})
			if err != nil {
				t.Fatalf("GetObject: %v", err)
			}
			if resp.Size != int64(tc.size) {
				t.Errorf("Size: got=%d want=%d", resp.Size, tc.size)
			}

			got, err := io.ReadAll(resp.Reader)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, body) {
				t.Errorf("roundtrip mismatch: got %d bytes want %d", len(got), len(body))
			}
		})
	}
}

func TestGetObjectMissing(t *testing.T) {
	testutil.SetupServer(t)
	_, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "ghost"})
	if err == nil {
		t.Fatal("GetObject missing: err=nil, want non-nil")
	}
}

func TestChunkReaderShortRead(t *testing.T) {
	testutil.SetupServer(t)

	body := bytes.Repeat([]byte("X"), 1024*3+128)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body)}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	resp, err := GetObject(&GetObjectRequest{Bucket: "b", Key: "obj"})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}

	var got bytes.Buffer
	buf := make([]byte, 7)
	for {
		n, err := resp.Reader.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if !bytes.Equal(got.Bytes(), body) {
		t.Errorf("short-read roundtrip mismatch: got %d want %d", got.Len(), len(body))
	}
}
