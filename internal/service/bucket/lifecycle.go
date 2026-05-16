package bucket

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

var ErrLifecycleNotFound = errors.New("lifecycle configuration not found")

type LifecycleRule struct {
	ID                       string `json:"id,omitempty"`
	Enabled                  bool   `json:"enabled"`
	Prefix                   string `json:"prefix,omitempty"`
	ExpirationDays           int    `json:"expiration_days,omitempty"`
	AbortMultipartAfterDays  int    `json:"abort_multipart_after_days,omitempty"`
}

type LifecycleConfiguration struct {
	Rules []LifecycleRule `json:"rules"`
}

func lifecycleKey(bucket string) []byte {
	return []byte(fmt.Sprintf("lifecycle:%s", bucket))
}

func PutLifecycle(bucket string, cfg *LifecycleConfiguration) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return db.Put(lifecycleKey(bucket), data)
}

func GetLifecycle(bucket string) (*LifecycleConfiguration, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(lifecycleKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrLifecycleNotFound
		}
		return nil, err
	}

	var cfg LifecycleConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func DeleteLifecycle(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.Delete(lifecycleKey(bucket))
}

func MatchLifecycleRule(cfg *LifecycleConfiguration, key string) *LifecycleRule {
	if cfg == nil {
		return nil
	}

	var best *LifecycleRule
	bestLen := -1
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if !r.Enabled || r.ExpirationDays <= 0 {
			continue
		}
		if r.Prefix == "" || strings.HasPrefix(key, r.Prefix) {
			if len(r.Prefix) > bestLen {
				best = r
				bestLen = len(r.Prefix)
			}
		}
	}
	return best
}
