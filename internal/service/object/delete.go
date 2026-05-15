package object

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/storage"
)

type DeleteObjectRequest struct {
	Bucket string
	Key    string
}

var ErrObjectNotFound = errors.New("object not found")

func DeleteObject(req *DeleteObjectRequest) error {
	meta, err := storage.DeleteMetadataFromBucket(req.Bucket, req.Key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return ErrObjectNotFound
		}
		return fmt.Errorf("failed to delete object: %w", err)
	}

	if err := storage.DecrementChunkRefs(meta.ChunkHashes); err != nil {
		return fmt.Errorf("failed to decrement chunk refs: %w", err)
	}
	return nil
}
