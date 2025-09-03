package object

import (
	"fmt"
	"strings"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/pkg/database"
	"github.com/anhostfr/hangar/pkg/pathutil"
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

// ListObjectsInBucket returns a list of objects in a specific bucket
func ListObjectsInBucket(bucket, prefix string) (*ListObjectsResponse, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var searchPrefix string
	if bucket != "" {
		searchPrefix = fmt.Sprintf("metadata:%s/", bucket)
	} else {
		searchPrefix = "metadata:"
	}

	iter, err := db.NewIteratorWithPrefix([]byte(searchPrefix))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var objects []ObjectInfo

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		objectKey := dbutils.ExtractFilenameFromKey(key)

		// Remove bucket prefix from object key for display
		if bucket != "" {
			if strings.HasPrefix(objectKey, bucket+"/") {
				objectKey = objectKey[len(bucket)+1:]
			}
		}

		if prefix != "" && !strings.HasPrefix(objectKey, prefix) {
			continue
		}

		metadata, err := storage.GetMetadataFromBucket(bucket, objectKey)
		if err != nil {
			continue // Skip corrupted metadata
		}

		objects = append(objects, ObjectInfo{
			Key:         objectKey,
			Filename:    pathutil.ExtractFilename(objectKey),
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
