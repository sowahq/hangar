package storage

import (
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/anhostfr/hangar/internal/database"
)

type Metadatas struct {
	Key            string   `json:"key"`
	ETag           string   `json:"etag"`
	Size           int64    `json:"size"`
	ContentType    string   `json:"content_type"`
	CreatedAt      int64    `json:"created_at"`
	ObjectHash     string   `json:"object_hash"`
	ChunkHashes    []string `json:"chunk_hashes"`
	VersionID      string   `json:"version_id,omitempty"`
	IsDeleteMarker bool     `json:"is_delete_marker,omitempty"`
}

func StoreMetadataInBucket(bucket string, metadata *Metadatas) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	var key []byte
	if bucket != "" {
		key = []byte(fmt.Sprintf("metadata:%s/%s", bucket, metadata.Key))
	} else {
		key = []byte(fmt.Sprintf("metadata:%s", metadata.Key))
	}

	if err := db.Put(key, data); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	return nil
}

func DeleteMetadataFromBucket(bucket, filename string) (*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var key []byte
	if bucket != "" {
		key = []byte(fmt.Sprintf("metadata:%s/%s", bucket, filename))
	} else {
		key = []byte(fmt.Sprintf("metadata:%s", filename))
	}

	data, err := db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, pebble.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata Metadatas
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	if err := db.Delete(key); err != nil {
		return nil, fmt.Errorf("failed to delete metadata: %w", err)
	}
	return &metadata, nil
}

func GetMetadataFromBucket(bucket, filename string) (*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var key []byte
	if bucket != "" {
		key = []byte(fmt.Sprintf("metadata:%s/%s", bucket, filename))
	} else {
		key = []byte(fmt.Sprintf("metadata:%s", filename))
	}

	data, err := db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, fmt.Errorf("metadata not found")
		}
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	var metadata Metadatas
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}
