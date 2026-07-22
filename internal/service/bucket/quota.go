package bucket

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sowahq/hangar/internal/database"
	"github.com/sowahq/hangar/internal/storage"
)

var ErrQuotaExceeded = errors.New("quota exceeded")

func GetUsage(name string) (int64, int64, error) {
	db := database.LocalStore()
	if db == nil {
		return 0, 0, fmt.Errorf("database not initialized")
	}
	prefix := []byte(fmt.Sprintf("metadata:%s/", name))
	iter, err := db.NewIteratorWithPrefix(prefix)
	if err != nil {
		return 0, 0, err
	}
	defer iter.Close()

	var bytesUsed int64
	var objects int64
	for iter.SeekGE(prefix); iter.Valid(); iter.Next() {
		k := iter.Key()
		if !bytes.HasPrefix(k, prefix) {
			break
		}
		var m storage.Metadatas
		if err := json.Unmarshal(iter.Value(), &m); err != nil {
			continue
		}
		bytesUsed += m.Size
		objects++
	}
	return bytesUsed, objects, nil
}

func UpdateQuota(name string, maxBytes, maxObjects int64) (*BucketInfo, error) {
	if maxBytes < 0 || maxObjects < 0 {
		return nil, fmt.Errorf("quota values must be >= 0")
	}
	info, err := GetBucket(name)
	if err != nil {
		return nil, err
	}
	info.MaxBytes = maxBytes
	info.MaxObjects = maxObjects
	info.UpdatedAt = time.Now().UnixMilli()

	data, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if err := db.Put([]byte(fmt.Sprintf("bucket:%s", name)), data); err != nil {
		return nil, err
	}
	return info, nil
}
