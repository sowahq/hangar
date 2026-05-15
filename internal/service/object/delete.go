package object

import (
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
)

type DeleteObjectRequest struct {
	Bucket    string
	Key       string
	VersionID string
}

type DeleteObjectResponse struct {
	VersionID      string `json:"version_id,omitempty"`
	IsDeleteMarker bool   `json:"is_delete_marker,omitempty"`
}

var ErrObjectNotFound = errors.New("object not found")

func DeleteObject(req *DeleteObjectRequest) (*DeleteObjectResponse, error) {
	info, _ := bucket.GetBucket(req.Bucket)
	versioning := info != nil && info.VersioningEnabled

	if req.VersionID != "" {
		v, err := storage.DeleteObjectVersion(req.Bucket, req.Key, req.VersionID)
		if err != nil {
			if errors.Is(err, storage.ErrVersionNotFound) {
				return nil, ErrObjectNotFound
			}
			return nil, fmt.Errorf("failed to delete version: %w", err)
		}
		if len(v.ChunkHashes) > 0 {
			if err := storage.DecrementChunkRefs(v.ChunkHashes); err != nil {
				return nil, fmt.Errorf("failed to decrement chunk refs: %w", err)
			}
		}
		cur, curErr := storage.GetMetadataFromBucket(req.Bucket, req.Key)
		if curErr == nil && cur.VersionID == req.VersionID {
			if _, err := storage.DeleteMetadataFromBucket(req.Bucket, req.Key); err != nil {
				return nil, fmt.Errorf("failed to delete current pointer: %w", err)
			}
		}
		return &DeleteObjectResponse{VersionID: v.VersionID, IsDeleteMarker: v.IsDeleteMarker}, nil
	}

	if versioning {
		marker := &storage.Metadatas{
			Key:            req.Key,
			VersionID:      newVersionID(),
			IsDeleteMarker: true,
			CreatedAt:      nowMillis(),
		}
		if err := storage.StoreObjectVersion(req.Bucket, marker); err != nil {
			return nil, fmt.Errorf("failed to store delete marker: %w", err)
		}
		cur, curErr := storage.GetMetadataFromBucket(req.Bucket, req.Key)
		if curErr == nil && !cur.IsDeleteMarker && cur.VersionID == "" && len(cur.ChunkHashes) > 0 {
			legacy := *cur
			legacy.VersionID = newVersionID()
			if storeErr := storage.StoreObjectVersion(req.Bucket, &legacy); storeErr != nil {
				return nil, fmt.Errorf("failed to archive legacy current as version: %w", storeErr)
			}
		}
		if err := storage.StoreMetadataInBucket(req.Bucket, marker); err != nil {
			return nil, fmt.Errorf("failed to update current pointer: %w", err)
		}
		return &DeleteObjectResponse{VersionID: marker.VersionID, IsDeleteMarker: true}, nil
	}

	meta, err := storage.DeleteMetadataFromBucket(req.Bucket, req.Key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("failed to delete object: %w", err)
	}

	if err := storage.DecrementChunkRefs(meta.ChunkHashes); err != nil {
		return nil, fmt.Errorf("failed to decrement chunk refs: %w", err)
	}
	return &DeleteObjectResponse{}, nil
}
