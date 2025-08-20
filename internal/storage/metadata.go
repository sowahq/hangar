package storage

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/database"
)

type Metadatas struct {
	Filename    string   `json:"filename"`
	ETag        string   `json:"etag"`
	Size        int64    `json:"size"`
	ContentType string   `json:"content_type"`
	CreatedAt   int64    `json:"created_at"`
	ObjectHash  string   `json:"object_hash"`
	ChunkHashes []string `json:"chunk_hashes"` // ordered by chunk index already, fucker
}

// StoreMetadata stores object metadata in the database
func StoreMetadata(metadata *Metadatas) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	key := []byte(fmt.Sprintf("metadata:%s", metadata.Filename))

	if err := db.Put(key, data); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	return nil
}

// GetMetadata retrieves object metadata from the database
func GetMetadata(filename string) (*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	key := []byte(fmt.Sprintf("metadata:%s", filename))

	data, err := db.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	var metadata Metadatas
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}
