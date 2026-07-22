package bucket

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

var ErrLoggingNotFound = errors.New("logging configuration not found")

type LoggingConfig struct {
	TargetBucket string `json:"target_bucket"`
	TargetPrefix string `json:"target_prefix,omitempty"`
}

func loggingKey(bucket string) []byte {
	return []byte(fmt.Sprintf("logging:%s", bucket))
}

func PutLogging(bucket string, cfg *LoggingConfig) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}
	if cfg.TargetBucket == "" {
		return errors.New("target_bucket required")
	}
	if _, err := GetBucket(cfg.TargetBucket); err != nil {
		return fmt.Errorf("target bucket: %w", err)
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal logging: %w", err)
	}
	return db.Put(loggingKey(bucket), data)
}

func GetLogging(bucket string) (*LoggingConfig, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(loggingKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrLoggingNotFound
		}
		return nil, fmt.Errorf("get logging: %w", err)
	}
	var cfg LoggingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal logging: %w", err)
	}
	return &cfg, nil
}

func DeleteLogging(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.Delete(loggingKey(bucket))
}
