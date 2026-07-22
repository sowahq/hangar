package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

const (
	mpuPrefix     = "mpu:"
	mpuPartPrefix = "mpupart:"
	mpuSep        = "\x00"
)

var (
	ErrMultipartNotFound = errors.New("multipart upload not found")
	ErrMultipartPartNotFound = errors.New("multipart part not found")
)

type MultipartHeader struct {
	UploadID  string `json:"upload_id"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	CreatedAt int64  `json:"created_at"`

	ContentType       string `json:"content_type,omitempty"`
	SSEAlgorithm      string `json:"sse_algorithm,omitempty"`
	SSECustomerKeyMD5 string `json:"sse_customer_key_md5,omitempty"`
	SSESalt           []byte `json:"sse_salt,omitempty"`
	SSENoncePrefix    []byte `json:"sse_nonce_prefix,omitempty"`
	SSEKeyID          string `json:"sse_key_id,omitempty"`
}

type MultipartPart struct {
	PartNumber  int      `json:"part_number"`
	Size        int64    `json:"size"`
	ETag        string   `json:"etag"`
	ChunkHashes []string `json:"chunk_hashes"`
	UploadedAt  int64    `json:"uploaded_at"`

	ChecksumAlgorithm string `json:"checksum_algorithm,omitempty"`
	ChecksumValue     string `json:"checksum_value,omitempty"`
}

func mpuKey(bucket, key, uploadID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s%s%s", mpuPrefix, bucket, key, mpuSep, uploadID))
}

func mpuPartKey(bucket, key, uploadID string, partNumber int) []byte {
	return []byte(fmt.Sprintf("%s%s/%s%s%s%s%05d", mpuPartPrefix, bucket, key, mpuSep, uploadID, mpuSep, partNumber))
}

func mpuPartListPrefix(bucket, key, uploadID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s%s%s%s", mpuPartPrefix, bucket, key, mpuSep, uploadID, mpuSep))
}

func mpuBucketPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("%s%s/", mpuPrefix, bucket))
}

func mpuPartBucketPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("%s%s/", mpuPartPrefix, bucket))
}

func StoreMultipartHeader(h *MultipartHeader) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("failed to marshal mpu header: %w", err)
	}
	return db.Put(mpuKey(h.Bucket, h.Key, h.UploadID), data)
}

func GetMultipartHeader(bucket, key, uploadID string) (*MultipartHeader, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(mpuKey(bucket, key, uploadID))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrMultipartNotFound
		}
		return nil, fmt.Errorf("failed to read mpu header: %w", err)
	}
	var h MultipartHeader
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mpu header: %w", err)
	}
	return &h, nil
}

func StoreMultipartPart(bucket, key, uploadID string, p *MultipartPart) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal mpu part: %w", err)
	}
	return db.Put(mpuPartKey(bucket, key, uploadID, p.PartNumber), data)
}

func GetMultipartPart(bucket, key, uploadID string, partNumber int) (*MultipartPart, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(mpuPartKey(bucket, key, uploadID, partNumber))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrMultipartPartNotFound
		}
		return nil, fmt.Errorf("failed to read mpu part: %w", err)
	}
	var p MultipartPart
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mpu part: %w", err)
	}
	return &p, nil
}

func ListMultipartParts(bucket, key, uploadID string) ([]*MultipartPart, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	prefix := mpuPartListPrefix(bucket, key, uploadID)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate mpu parts: %w", err)
	}
	defer iter.Close()

	var parts []*MultipartPart
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		var p MultipartPart
		if err := json.Unmarshal(iter.Value(), &p); err != nil {
			continue
		}
		pc := p
		parts = append(parts, &pc)
	}
	return parts, nil
}

func DeleteMultipart(bucket, key, uploadID string) ([][]byte, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var toDelete [][]byte
	toDelete = append(toDelete, mpuKey(bucket, key, uploadID))

	prefix := mpuPartListPrefix(bucket, key, uploadID)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate mpu parts: %w", err)
	}
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		keyCopy := make([]byte, len(iter.Key()))
		copy(keyCopy, iter.Key())
		toDelete = append(toDelete, keyCopy)
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close iterator: %w", err)
	}

	if err := db.DeleteBatch(toDelete); err != nil {
		return nil, fmt.Errorf("failed to delete mpu records: %w", err)
	}
	return toDelete, nil
}

func ScanBucketMultiparts(bucket string) ([]*MultipartHeader, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	prefix := mpuBucketPrefix(bucket)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate mpu headers: %w", err)
	}
	defer iter.Close()

	var out []*MultipartHeader
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		var h MultipartHeader
		if err := json.Unmarshal(iter.Value(), &h); err != nil {
			continue
		}
		hc := h
		out = append(out, &hc)
	}
	return out, nil
}

func ScanBucketMultipartParts(bucket string) ([]*MultipartPart, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	prefix := mpuPartBucketPrefix(bucket)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate mpu parts: %w", err)
	}
	defer iter.Close()

	var parts []*MultipartPart
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		var p MultipartPart
		if err := json.Unmarshal(iter.Value(), &p); err != nil {
			continue
		}
		pc := p
		parts = append(parts, &pc)
	}
	return parts, nil
}
