package bucket

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

var ErrWebsiteNotFound = errors.New("website configuration not found")

type WebsiteConfig struct {
	IndexDocument string `json:"index_document"`
	ErrorDocument string `json:"error_document,omitempty"`
}

func websiteKey(bucket string) []byte {
	return []byte(fmt.Sprintf("website:%s", bucket))
}

func PutWebsite(bucket string, cfg *WebsiteConfig) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}
	if cfg.IndexDocument == "" {
		return errors.New("index_document required")
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal website: %w", err)
	}
	return db.Put(websiteKey(bucket), data)
}

func GetWebsite(bucket string) (*WebsiteConfig, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(websiteKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrWebsiteNotFound
		}
		return nil, fmt.Errorf("get website: %w", err)
	}
	var cfg WebsiteConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal website: %w", err)
	}
	return &cfg, nil
}

func DeleteWebsite(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.Delete(websiteKey(bucket))
}
