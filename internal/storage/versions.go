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
	versionPrefix = "version:"
	versionSep    = "\x00"
)

var ErrVersionNotFound = errors.New("version not found")

func versionKey(bucket, key, versionID string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s%s%s", versionPrefix, bucket, key, versionSep, versionID))
}

func versionListPrefix(bucket, key string) []byte {
	return []byte(fmt.Sprintf("%s%s/%s%s", versionPrefix, bucket, key, versionSep))
}

func versionBucketPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("%s%s/", versionPrefix, bucket))
}

func StoreObjectVersion(bucket string, m *Metadatas) error {
	if m.VersionID == "" {
		return fmt.Errorf("version id required")
	}
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}
	if err := db.Put(versionKey(bucket, m.Key, m.VersionID), data); err != nil {
		return fmt.Errorf("failed to store version: %w", err)
	}
	return nil
}

func GetObjectVersion(bucket, key, versionID string) (*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(versionKey(bucket, key, versionID))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrVersionNotFound
		}
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	var m Metadatas
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version: %w", err)
	}
	return &m, nil
}

func ListObjectVersions(bucket, key string) ([]*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	prefix := versionListPrefix(bucket, key)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to iterate versions: %w", err)
	}
	defer iter.Close()

	var out []*Metadatas
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		var m Metadatas
		if err := json.Unmarshal(iter.Value(), &m); err != nil {
			continue
		}
		mc := m
		out = append(out, &mc)
	}
	return out, nil
}

func DeleteObjectVersion(bucket, key, versionID string) (*Metadatas, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	k := versionKey(bucket, key, versionID)
	data, err := db.Get(k)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrVersionNotFound
		}
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	var m Metadatas
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version: %w", err)
	}
	if err := db.Delete(k); err != nil {
		return nil, fmt.Errorf("failed to delete version: %w", err)
	}
	return &m, nil
}

func ScanBucketVersions(bucket string) ([]*Metadatas, [][]byte, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, nil, fmt.Errorf("database not initialized")
	}
	prefix := versionBucketPrefix(bucket)
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to iterate versions: %w", err)
	}
	defer iter.Close()

	var metas []*Metadatas
	var keys [][]byte
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}
		var m Metadatas
		if err := json.Unmarshal(iter.Value(), &m); err != nil {
			continue
		}
		mc := m
		metas = append(metas, &mc)
		keyCopy := make([]byte, len(iter.Key()))
		copy(keyCopy, iter.Key())
		keys = append(keys, keyCopy)
	}
	return metas, keys, nil
}
