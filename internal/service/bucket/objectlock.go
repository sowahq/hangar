package bucket

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

const (
	ObjectLockModeGovernance = "GOVERNANCE"
	ObjectLockModeCompliance = "COMPLIANCE"
)

var (
	ErrObjectLockNotConfigured  = errors.New("object lock not configured for this bucket")
	ErrObjectLockNeedsVersion   = errors.New("object lock requires versioning to be enabled")
	ErrObjectLockCannotDisable  = errors.New("object lock cannot be disabled once enabled")
	ErrObjectLockInvalidMode    = errors.New("invalid object lock mode")
	ErrObjectLockInvalidRetain  = errors.New("invalid object lock retention")
)

type DefaultRetention struct {
	Mode  string `json:"mode,omitempty"`
	Days  int    `json:"days,omitempty"`
	Years int    `json:"years,omitempty"`
}

type ObjectLockConfig struct {
	Enabled          bool              `json:"enabled"`
	DefaultRetention *DefaultRetention `json:"default_retention,omitempty"`
}

func objectLockKey(bucket string) []byte {
	return []byte(fmt.Sprintf("objectlock:%s", bucket))
}

func ValidateLockMode(mode string) error {
	switch mode {
	case ObjectLockModeGovernance, ObjectLockModeCompliance, "":
		return nil
	}
	return ErrObjectLockInvalidMode
}

func PutObjectLockConfig(bucketName string, cfg *ObjectLockConfig) error {
	info, err := GetBucket(bucketName)
	if err != nil {
		return err
	}

	if !cfg.Enabled {
		return ErrObjectLockInvalidMode
	}

	if !info.VersioningEnabled {
		return ErrObjectLockNeedsVersion
	}

	if cfg.DefaultRetention != nil {
		if err := ValidateLockMode(cfg.DefaultRetention.Mode); err != nil {
			return err
		}
		if cfg.DefaultRetention.Days < 0 || cfg.DefaultRetention.Years < 0 {
			return ErrObjectLockInvalidRetain
		}
		if cfg.DefaultRetention.Days == 0 && cfg.DefaultRetention.Years == 0 {
			return ErrObjectLockInvalidRetain
		}
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	if !info.ObjectLockEnabled {
		info.ObjectLockEnabled = true
		info.UpdatedAt = time.Now().UnixMilli()

		biData, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("marshal bucket: %w", err)
		}
		if err := db.Put([]byte(fmt.Sprintf("bucket:%s", bucketName)), biData); err != nil {
			return fmt.Errorf("persist bucket flag: %w", err)
		}
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal objectlock: %w", err)
	}

	return db.Put(objectLockKey(bucketName), data)
}

func GetObjectLockConfig(bucketName string) (*ObjectLockConfig, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(objectLockKey(bucketName))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrObjectLockNotConfigured
		}
		return nil, fmt.Errorf("get objectlock: %w", err)
	}

	var cfg ObjectLockConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal objectlock: %w", err)
	}
	return &cfg, nil
}

func DeleteObjectLockConfig(bucketName string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.Delete(objectLockKey(bucketName))
}
