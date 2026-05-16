package bucket

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

var ErrEncryptionNotFound = errors.New("encryption configuration not found")

type EncryptionConfig struct {
	Algorithm string `json:"algorithm"`
	KMSKeyID  string `json:"kms_key_id,omitempty"`
}

func encryptionKey(bucket string) []byte {
	return []byte(fmt.Sprintf("encryption:%s", bucket))
}

func PutEncryption(bucket string, cfg *EncryptionConfig) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal encryption: %w", err)
	}

	return db.Put(encryptionKey(bucket), data)
}

func GetEncryption(bucket string) (*EncryptionConfig, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(encryptionKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrEncryptionNotFound
		}
		return nil, fmt.Errorf("get encryption: %w", err)
	}

	var cfg EncryptionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal encryption: %w", err)
	}

	return &cfg, nil
}

func DeleteEncryption(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	return db.Delete(encryptionKey(bucket))
}
