package object

import (
	"bytes"
	"io"
	"testing"

	bucketSvc "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/internal/testutil"
)

func setupMpuBucket(t *testing.T, name string) {
	t.Helper()
	if _, err := bucketSvc.CreateBucket(&bucketSvc.CreateBucketRequest{Name: name}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
}

func TestMultipartRoundtrip(t *testing.T) {
	testutil.SetupServer(t)
	setupMpuBucket(t, "mbucket")

	init, err := InitiateMultipart(&InitiateMultipartRequest{Bucket: "mbucket", Key: "big.bin"})
	if err != nil {
		t.Fatalf("InitiateMultipart: %v", err)
	}
	if init.UploadID == "" {
		t.Fatal("empty upload id")
	}

	parts := [][]byte{
		bytes.Repeat([]byte("A"), 2048),
		bytes.Repeat([]byte("B"), 1500),
		bytes.Repeat([]byte("C"), 500),
	}
	for i, p := range parts {
		if _, err := UploadPart(&UploadPartRequest{
			Bucket:     "mbucket",
			Key:        "big.bin",
			UploadID:   init.UploadID,
			PartNumber: i + 1,
			Body:       bytes.NewReader(p),
		}); err != nil {
			t.Fatalf("UploadPart %d: %v", i+1, err)
		}
	}

	res, err := CompleteMultipart(&CompleteMultipartRequest{
		Bucket:   "mbucket",
		Key:      "big.bin",
		UploadID: init.UploadID,
		Parts:    []int{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("CompleteMultipart: %v", err)
	}
	wantSize := int64(0)
	for _, p := range parts {
		wantSize += int64(len(p))
	}
	if res.Size != wantSize {
		t.Errorf("Size: got=%d want=%d", res.Size, wantSize)
	}

	got, err := GetObject(&GetObjectRequest{Bucket: "mbucket", Key: "big.bin"})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	all, err := io.ReadAll(got.Reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var want []byte
	for _, p := range parts {
		want = append(want, p...)
	}
	if !bytes.Equal(all, want) {
		t.Fatalf("content mismatch: got %d bytes want %d bytes", len(all), len(want))
	}

	if _, err := storage.GetMultipartHeader("mbucket", "big.bin", init.UploadID); err == nil {
		t.Error("mpu header should be cleaned up after Complete")
	}
}

func TestMultipartAbortFreesChunks(t *testing.T) {
	testutil.SetupServer(t)
	setupMpuBucket(t, "mbucket")

	init, err := InitiateMultipart(&InitiateMultipartRequest{Bucket: "mbucket", Key: "k"})
	if err != nil {
		t.Fatalf("InitiateMultipart: %v", err)
	}

	body := bytes.Repeat([]byte("Z"), 3000)
	if _, err := UploadPart(&UploadPartRequest{
		Bucket: "mbucket", Key: "k", UploadID: init.UploadID, PartNumber: 1,
		Body: bytes.NewReader(body),
	}); err != nil {
		t.Fatalf("UploadPart: %v", err)
	}

	parts, err := storage.ListMultipartParts("mbucket", "k", init.UploadID)
	if err != nil {
		t.Fatalf("ListMultipartParts: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}
	chunks := parts[0].ChunkHashes

	if err := AbortMultipart(&AbortMultipartRequest{Bucket: "mbucket", Key: "k", UploadID: init.UploadID}); err != nil {
		t.Fatalf("AbortMultipart: %v", err)
	}

	if _, err := storage.GetMultipartHeader("mbucket", "k", init.UploadID); err == nil {
		t.Error("mpu header should be gone after Abort")
	}

	for _, h := range chunks {
		ref, _ := storage.IsChunkReferenced(h)
		if ref {
			t.Errorf("chunk %s still referenced after Abort", h)
		}
	}
}

func TestMultipartInvalidPartNumber(t *testing.T) {
	testutil.SetupServer(t)
	setupMpuBucket(t, "mbucket")

	init, _ := InitiateMultipart(&InitiateMultipartRequest{Bucket: "mbucket", Key: "k"})

	tests := []struct {
		name string
		n    int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too big", MaxPartNumber + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UploadPart(&UploadPartRequest{
				Bucket: "mbucket", Key: "k", UploadID: init.UploadID, PartNumber: tt.n,
				Body: bytes.NewReader([]byte("x")),
			})
			if err != ErrInvalidPartNumber {
				t.Errorf("err=%v want=ErrInvalidPartNumber", err)
			}
		})
	}
}

func TestMultipartUnknownUploadID(t *testing.T) {
	testutil.SetupServer(t)
	setupMpuBucket(t, "mbucket")

	_, err := UploadPart(&UploadPartRequest{
		Bucket: "mbucket", Key: "k", UploadID: "deadbeef", PartNumber: 1,
		Body: bytes.NewReader([]byte("x")),
	})
	if err != ErrMultipartNotFound {
		t.Errorf("err=%v want=ErrMultipartNotFound", err)
	}
}
