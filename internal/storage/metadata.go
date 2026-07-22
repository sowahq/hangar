package storage

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
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
	PartSizes      []int64  `json:"part_sizes,omitempty"`

	SSEAlgorithm        string  `json:"sse_algorithm,omitempty"`
	SSECustomerKeyMD5   string  `json:"sse_customer_key_md5,omitempty"`
	SSESalt             []byte  `json:"sse_salt,omitempty"`
	SSENoncePrefix      []byte  `json:"sse_nonce_prefix,omitempty"`
	SSEPartNumbers      []int   `json:"sse_part_numbers,omitempty"`
	SSEPartChunkCounts  []int   `json:"sse_part_chunk_counts,omitempty"`
	SSEKeyID            string  `json:"sse_key_id,omitempty"`

	ChecksumAlgorithm string `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string `json:"checksum_value,omitempty"`

	ObjectLockMode             string `json:"object_lock_mode,omitempty"`
	ObjectLockRetainUntilMilli int64  `json:"object_lock_retain_until_milli,omitempty"`
	ObjectLockLegalHold        bool   `json:"object_lock_legal_hold,omitempty"`

	Tags []Tag `json:"tags,omitempty"`
}

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func StoreMetadataInBucket(bucket string, metadata *Metadatas) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := ActiveMetadataStore().PutRaw(bucket, metadata.Key, data); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}
	return nil
}

func DeleteMetadataFromBucket(bucket, filename string) (*Metadatas, error) {
	data, err := ActiveMetadataStore().DeleteRaw(bucket, filename)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, pebble.ErrNotFound
		}
		return nil, fmt.Errorf("failed to delete metadata: %w", err)
	}
	var metadata Metadatas
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return &metadata, nil
}

func GetMetadataFromBucket(bucket, filename string) (*Metadatas, error) {
	data, err := ActiveMetadataStore().GetRaw(bucket, filename)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
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
