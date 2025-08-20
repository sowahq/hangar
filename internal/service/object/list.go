package object

import (
	"fmt"
	"strings"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/internal/utils/database"
	"github.com/anhostfr/hangar/internal/utils/path"
)

type ObjectInfo struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	ETag        string `json:"etag"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	CreatedAt   int64  `json:"created_at"`
	ObjectHash  string `json:"object_hash"`
}

type ListObjectsResponse struct {
	Objects []ObjectInfo `json:"objects"`
	Count   int          `json:"count"`
}

// ListObjects returns a list of stored objects, optionally filtered by prefix
func ListObjects(prefix string) (*ListObjectsResponse, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	iter, err := db.NewIteratorWithPrefix([]byte("metadata:"))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var objects []ObjectInfo

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		objectKey := dbutils.ExtractFilenameFromKey(key)

		if prefix != "" && !strings.HasPrefix(objectKey, prefix) {
			continue
		}

		metadata, err := storage.GetMetadata(objectKey)
		if err != nil {
			continue // Skip corrupted metadata
		}

		objects = append(objects, ObjectInfo{
			Key:         objectKey,
			Filename:    path.ExtractFilename(objectKey),
			ETag:        metadata.ETag,
			Size:        metadata.Size,
			ContentType: metadata.ContentType,
			CreatedAt:   metadata.CreatedAt,
			ObjectHash:  metadata.ObjectHash,
		})
	}

	return &ListObjectsResponse{
		Objects: objects,
		Count:   len(objects),
	}, nil
}
