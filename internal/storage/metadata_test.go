package storage

import (
	"errors"
	"testing"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/testutil"
)

func TestDeleteMetadataFromBucket(t *testing.T) {
	tests := []struct {
		name     string
		seed     *Metadatas
		bucket   string
		key      string
		wantErr  error
		wantMeta *Metadatas
	}{
		{
			name:    "missing object returns ErrNotFound",
			bucket:  "b",
			key:     "ghost",
			wantErr: pebble.ErrNotFound,
		},
		{
			name:     "existing object returns deleted metadata with chunks",
			seed:     &Metadatas{Key: "obj", ChunkHashes: []string{"h1", "h2", "h3"}, Size: 42},
			bucket:   "b",
			key:      "obj",
			wantMeta: &Metadatas{Key: "obj", ChunkHashes: []string{"h1", "h2", "h3"}, Size: 42},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			if tc.seed != nil {
				if err := StoreMetadataInBucket(tc.bucket, tc.seed); err != nil {
					t.Fatalf("StoreMetadataInBucket: %v", err)
				}
			}

			got, err := DeleteMetadataFromBucket(tc.bucket, tc.key)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err=%v want=%v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.Key != tc.wantMeta.Key || got.Size != tc.wantMeta.Size {
				t.Errorf("metadata: got=%+v want=%+v", got, tc.wantMeta)
			}
			if len(got.ChunkHashes) != len(tc.wantMeta.ChunkHashes) {
				t.Fatalf("ChunkHashes len: got=%d want=%d", len(got.ChunkHashes), len(tc.wantMeta.ChunkHashes))
			}
			for i, h := range got.ChunkHashes {
				if h != tc.wantMeta.ChunkHashes[i] {
					t.Errorf("ChunkHashes[%d]: got=%s want=%s", i, h, tc.wantMeta.ChunkHashes[i])
				}
			}

			_, err = GetMetadataFromBucket(tc.bucket, tc.key)
			if err == nil {
				t.Errorf("metadata still readable after delete")
			}
		})
	}
}
