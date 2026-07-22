package bucket

import (
	"testing"

	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/internal/testutil"
)

func TestDeleteBucketForceDecrementsChunkRefs(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "mybucket"}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	objects := []*storage.Metadatas{
		{Key: "obj1", ChunkHashes: []string{"h1", "h2"}},
		{Key: "obj2", ChunkHashes: []string{"h2", "h3"}},
	}
	for _, m := range objects {
		if err := storage.StoreMetadataInBucket("mybucket", m); err != nil {
			t.Fatalf("StoreMetadataInBucket: %v", err)
		}
		if err := storage.IncrementChunkRefs(m.ChunkHashes); err != nil {
			t.Fatalf("IncrementChunkRefs: %v", err)
		}
	}

	for _, hash := range []string{"h1", "h2", "h3"} {
		ref, err := storage.IsChunkReferenced(hash)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", hash, err)
		}
		if !ref {
			t.Fatalf("hash %s: not referenced before delete", hash)
		}
	}

	if err := DeleteBucket(&DeleteBucketRequest{Name: "mybucket", Force: true}); err != nil {
		t.Fatalf("DeleteBucket force=true: %v", err)
	}

	for _, hash := range []string{"h1", "h2", "h3"} {
		ref, err := storage.IsChunkReferenced(hash)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", hash, err)
		}
		if ref {
			t.Errorf("hash %s: still referenced after force-delete, want gone", hash)
		}
	}
}

func TestDeleteBucketEmptyDoesNotTouchOtherBucket(t *testing.T) {
	testutil.SetupDB(t)

	for _, name := range []string{"alpha", "beta"} {
		if _, err := CreateBucket(&CreateBucketRequest{Name: name}); err != nil {
			t.Fatalf("CreateBucket(%s): %v", name, err)
		}
	}

	betaMeta := &storage.Metadatas{Key: "obj", ChunkHashes: []string{"keep1", "keep2"}}
	if err := storage.StoreMetadataInBucket("beta", betaMeta); err != nil {
		t.Fatalf("StoreMetadataInBucket: %v", err)
	}
	if err := storage.IncrementChunkRefs(betaMeta.ChunkHashes); err != nil {
		t.Fatalf("IncrementChunkRefs: %v", err)
	}

	if err := DeleteBucket(&DeleteBucketRequest{Name: "alpha"}); err != nil {
		t.Fatalf("DeleteBucket(alpha): %v", err)
	}

	for _, h := range []string{"keep1", "keep2"} {
		ref, err := storage.IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("IsChunkReferenced(%s): %v", h, err)
		}
		if !ref {
			t.Errorf("hash %s: refcount dropped while deleting unrelated bucket", h)
		}
	}
}

func TestDeleteBucketNonEmptyWithoutForceRefuses(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "mybucket"}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if err := storage.StoreMetadataInBucket("mybucket", &storage.Metadatas{Key: "obj", ChunkHashes: []string{"h1"}}); err != nil {
		t.Fatalf("StoreMetadataInBucket: %v", err)
	}
	if err := storage.IncrementChunkRefs([]string{"h1"}); err != nil {
		t.Fatalf("IncrementChunkRefs: %v", err)
	}

	if err := DeleteBucket(&DeleteBucketRequest{Name: "mybucket", Force: false}); err == nil {
		t.Fatal("DeleteBucket without force succeeded on non-empty bucket, want error")
	}

	ref, err := storage.IsChunkReferenced("h1")
	if err != nil {
		t.Fatalf("IsChunkReferenced: %v", err)
	}
	if !ref {
		t.Error("chunkref dropped despite refused delete")
	}
}
