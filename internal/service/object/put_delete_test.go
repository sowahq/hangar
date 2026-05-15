package object

import (
	"bytes"
	"errors"
	"testing"

	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/internal/testutil"
)

func TestPutObjectIncrementsChunkRefs(t *testing.T) {
	testutil.SetupServer(t)

	body := bytes.Repeat([]byte("A"), 3500)
	resp, err := PutObject(&PutObjectRequest{
		Bucket: "b",
		Key:    "obj",
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if resp.Size != int64(len(body)) {
		t.Errorf("Size: got=%d want=%d", resp.Size, len(body))
	}

	meta, err := storage.GetMetadataFromBucket("b", "obj")
	if err != nil {
		t.Fatalf("GetMetadataFromBucket: %v", err)
	}
	if len(meta.ChunkHashes) == 0 {
		t.Fatal("metadata has no chunk hashes")
	}
	for _, h := range meta.ChunkHashes {
		ref, err := storage.IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", h, err)
		}
		if !ref {
			t.Errorf("chunk %s not referenced after PUT", h)
		}
	}
}

func TestDeleteObjectDecrementsChunkRefs(t *testing.T) {
	testutil.SetupServer(t)

	body := bytes.Repeat([]byte("B"), 3500)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj", Body: bytes.NewReader(body)}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	meta, err := storage.GetMetadataFromBucket("b", "obj")
	if err != nil {
		t.Fatalf("GetMetadataFromBucket: %v", err)
	}
	chunkHashes := append([]string{}, meta.ChunkHashes...)

	if _, err := DeleteObject(&DeleteObjectRequest{Bucket: "b", Key: "obj"}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}

	for _, h := range chunkHashes {
		ref, err := storage.IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", h, err)
		}
		if ref {
			t.Errorf("chunk %s still referenced after DELETE", h)
		}
	}
}

func TestDeleteObjectMissingReturnsErrObjectNotFound(t *testing.T) {
	testutil.SetupServer(t)
	_, err := DeleteObject(&DeleteObjectRequest{Bucket: "b", Key: "ghost"})
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("err=%v want=ErrObjectNotFound", err)
	}
}

func TestPutObjectDedupSharedChunksRefcount(t *testing.T) {
	testutil.SetupServer(t)

	body := bytes.Repeat([]byte("C"), 3500)
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj1", Body: bytes.NewReader(body)}); err != nil {
		t.Fatalf("PutObject obj1: %v", err)
	}
	if _, err := PutObject(&PutObjectRequest{Bucket: "b", Key: "obj2", Body: bytes.NewReader(body)}); err != nil {
		t.Fatalf("PutObject obj2: %v", err)
	}

	meta, err := storage.GetMetadataFromBucket("b", "obj1")
	if err != nil {
		t.Fatalf("GetMetadataFromBucket: %v", err)
	}

	if _, err := DeleteObject(&DeleteObjectRequest{Bucket: "b", Key: "obj1"}); err != nil {
		t.Fatalf("DeleteObject obj1: %v", err)
	}

	for _, h := range meta.ChunkHashes {
		ref, err := storage.IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", h, err)
		}
		if !ref {
			t.Errorf("chunk %s lost ref while obj2 still references it", h)
		}
	}

	if _, err := DeleteObject(&DeleteObjectRequest{Bucket: "b", Key: "obj2"}); err != nil {
		t.Fatalf("DeleteObject obj2: %v", err)
	}
	for _, h := range meta.ChunkHashes {
		ref, err := storage.IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", h, err)
		}
		if ref {
			t.Errorf("chunk %s still referenced after both deletes", h)
		}
	}
}
