package bucket

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/cockroachdb/pebble"
)

type BucketInfo struct {
	Name              string `json:"name"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	Public            bool   `json:"public"`
	MaxBytes          int64  `json:"max_bytes"`
	MaxObjects        int64  `json:"max_objects"`
	VersioningEnabled bool   `json:"versioning_enabled,omitempty"`
}

type CreateBucketRequest struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

type CreateBucketResponse struct {
	*BucketInfo
}

type ListBucketsResponse struct {
	Buckets []BucketInfo `json:"buckets"`
	Count   int          `json:"count"`
}

type DeleteBucketRequest struct {
	Name  string `json:"name"`
	Force bool   `json:"force"`
}

func CreateBucket(req *CreateBucketRequest) (*CreateBucketResponse, error) {
	if err := BucketName(req.Name); err != nil {
		return nil, err
	}

	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	key := []byte(fmt.Sprintf("bucket:%s", req.Name))

	exists, err := db.Exist(key)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if exists {
		return nil, fmt.Errorf("bucket already exists: %s", req.Name)
	}

	now := time.Now().UnixMilli()
	bucket := &BucketInfo{
		Name:      req.Name,
		CreatedAt: now,
		UpdatedAt: now,
		Public:    req.Public,
	}

	data, err := json.Marshal(bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal bucket: %w", err)
	}

	if err := db.Put(key, data); err != nil {
		return nil, fmt.Errorf("failed to store bucket: %w", err)
	}

	return &CreateBucketResponse{BucketInfo: bucket}, nil
}

func ListBuckets() (*ListBucketsResponse, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	iter, err := db.NewIteratorWithPrefix([]byte("bucket:"))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var buckets []BucketInfo

	for iter.First(); iter.Valid(); iter.Next() {
		var bucket BucketInfo
		if err := json.Unmarshal(iter.Value(), &bucket); err != nil {
			continue
		}

		buckets = append(buckets, bucket)
	}

	return &ListBucketsResponse{
		Buckets: buckets,
		Count:   len(buckets),
	}, nil
}

func GetBucket(name string) (*BucketInfo, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	key := []byte(fmt.Sprintf("bucket:%s", name))

	data, err := db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, fmt.Errorf("bucket not found: %s", name)
		}
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	var bucket BucketInfo
	if err := json.Unmarshal(data, &bucket); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bucket: %w", err)
	}

	return &bucket, nil
}

func DeleteBucket(req *DeleteBucketRequest) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	bucketKey := []byte(fmt.Sprintf("bucket:%s", req.Name))

	exists, err := db.Exist(bucketKey)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("bucket not found: %s", req.Name)
	}

	metaPrefix := []byte(fmt.Sprintf("metadata:%s/", req.Name))

	iter, err := db.NewIteratorWithPrefix(metaPrefix)
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}

	var metaKeys [][]byte
	var chunkHashes []string
	for iter.SeekGE(metaPrefix); iter.Valid(); iter.Next() {
		k := iter.Key()
		if !bytes.HasPrefix(k, metaPrefix) {
			break
		}
		if !req.Force {
			iter.Close()
			return fmt.Errorf("bucket not empty: %s. Use force=true to delete", req.Name)
		}
		keyCopy := make([]byte, len(k))
		copy(keyCopy, k)
		metaKeys = append(metaKeys, keyCopy)

		var meta storage.Metadatas
		if err := json.Unmarshal(iter.Value(), &meta); err != nil {
			continue
		}
		if meta.VersionID == "" {
			chunkHashes = append(chunkHashes, meta.ChunkHashes...)
		}
	}
	iter.Close()

	if !req.Force {
		versions, _, vErr := storage.ScanBucketVersions(req.Name)
		if vErr != nil {
			return fmt.Errorf("failed to scan versions: %w", vErr)
		}
		if len(versions) > 0 {
			return fmt.Errorf("bucket not empty: %s. Use force=true to delete", req.Name)
		}
		mpus, mErr := storage.ScanBucketMultiparts(req.Name)
		if mErr != nil {
			return fmt.Errorf("failed to scan multiparts: %w", mErr)
		}
		if len(mpus) > 0 {
			return fmt.Errorf("bucket has pending multipart uploads: %s. Use force=true to delete", req.Name)
		}
	}

	if req.Force {
		versions, versionKeys, vErr := storage.ScanBucketVersions(req.Name)
		if vErr != nil {
			return fmt.Errorf("failed to scan versions: %w", vErr)
		}
		for _, v := range versions {
			chunkHashes = append(chunkHashes, v.ChunkHashes...)
		}
		if len(versionKeys) > 0 {
			if err := db.DeleteBatch(versionKeys); err != nil {
				return fmt.Errorf("failed to delete version records: %w", err)
			}
		}

		mpuParts, mpErr := storage.ScanBucketMultipartParts(req.Name)
		if mpErr != nil {
			return fmt.Errorf("failed to scan mpu parts: %w", mpErr)
		}
		for _, p := range mpuParts {
			chunkHashes = append(chunkHashes, p.ChunkHashes...)
		}
		mpus, mpuErr := storage.ScanBucketMultiparts(req.Name)
		if mpuErr != nil {
			return fmt.Errorf("failed to scan mpu headers: %w", mpuErr)
		}
		for _, h := range mpus {
			if _, err := storage.DeleteMultipart(h.Bucket, h.Key, h.UploadID); err != nil {
				return fmt.Errorf("failed to delete mpu state: %w", err)
			}
		}
	}

	if len(metaKeys) > 0 {
		if err := db.DeleteBatch(metaKeys); err != nil {
			return fmt.Errorf("failed to delete bucket metadata: %w", err)
		}
	}
	if len(chunkHashes) > 0 {
		if err := storage.DecrementChunkRefs(chunkHashes); err != nil {
			return fmt.Errorf("failed to decrement chunk refs: %w", err)
		}
	}

	return db.Delete(bucketKey)
}
