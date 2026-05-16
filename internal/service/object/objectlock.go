package object

import (
	"errors"
	"fmt"
	"time"

	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
)

var (
	ErrObjectLockBucketDisabled = errors.New("object lock not enabled on bucket")
	ErrObjectLockInvalidArgs    = errors.New("invalid object lock arguments")
	ErrObjectLockShortenDenied  = errors.New("compliance retention cannot be shortened or removed")
	ErrObjectLockModeDowngrade  = errors.New("compliance mode cannot be downgraded to governance")
	ErrObjectLockHeld           = errors.New("object retention or legal hold prevents this action")
)

func RetentionBlocks(m *storage.Metadatas, bypassGovernance bool, now int64) error {
	if m == nil {
		return nil
	}
	if m.ObjectLockLegalHold {
		return ErrObjectLockHeld
	}
	if m.ObjectLockMode == "" || m.ObjectLockRetainUntilMilli <= now {
		return nil
	}
	if m.ObjectLockMode == bucket.ObjectLockModeCompliance {
		return ErrObjectLockHeld
	}
	if m.ObjectLockMode == bucket.ObjectLockModeGovernance && !bypassGovernance {
		return ErrObjectLockHeld
	}
	return nil
}

type RetentionInput struct {
	Mode             string
	RetainUntilMilli int64
}

func ValidateRetentionInput(in *RetentionInput, now int64) error {
	if in == nil {
		return ErrObjectLockInvalidArgs
	}
	if err := bucket.ValidateLockMode(in.Mode); err != nil {
		return err
	}
	if in.Mode == "" {
		return ErrObjectLockInvalidArgs
	}
	if in.RetainUntilMilli <= now {
		return ErrObjectLockInvalidArgs
	}
	return nil
}

func ApplyDefaultRetentionIfMissing(bucketName string, in *RetentionInput) (*RetentionInput, error) {
	if in != nil && in.Mode != "" {
		return in, nil
	}

	cfg, err := bucket.GetObjectLockConfig(bucketName)
	if err != nil || cfg == nil || cfg.DefaultRetention == nil {
		return in, nil
	}

	dr := cfg.DefaultRetention
	if dr.Mode == "" {
		return in, nil
	}

	dur := time.Duration(dr.Days) * 24 * time.Hour
	if dr.Years > 0 {
		dur += time.Duration(dr.Years) * 365 * 24 * time.Hour
	}
	if dur <= 0 {
		return in, nil
	}

	return &RetentionInput{
		Mode:             dr.Mode,
		RetainUntilMilli: time.Now().Add(dur).UnixMilli(),
	}, nil
}

func mutateMetadata(bucketName, key, versionID string, mutator func(*storage.Metadatas) error) (*storage.Metadatas, error) {
	if versionID != "" {
		m, err := storage.GetObjectVersion(bucketName, key, versionID)
		if err != nil {
			return nil, err
		}
		if err := mutator(m); err != nil {
			return nil, err
		}
		if err := storage.StoreObjectVersion(bucketName, m); err != nil {
			return nil, fmt.Errorf("store version: %w", err)
		}

		cur, curErr := storage.GetMetadataFromBucket(bucketName, key)
		if curErr == nil && cur.VersionID == versionID {
			if err := mutator(cur); err == nil {
				_ = storage.StoreMetadataInBucket(bucketName, cur)
			}
		}
		return m, nil
	}

	m, err := storage.GetMetadataFromBucket(bucketName, key)
	if err != nil {
		return nil, err
	}
	if err := mutator(m); err != nil {
		return nil, err
	}
	if err := storage.StoreMetadataInBucket(bucketName, m); err != nil {
		return nil, fmt.Errorf("store metadata: %w", err)
	}

	if m.VersionID != "" {
		v, vErr := storage.GetObjectVersion(bucketName, key, m.VersionID)
		if vErr == nil {
			if err := mutator(v); err == nil {
				_ = storage.StoreObjectVersion(bucketName, v)
			}
		}
	}
	return m, nil
}

func PutObjectRetention(bucketName, key, versionID string, in *RetentionInput, bypassGovernance bool) error {
	info, err := bucket.GetBucket(bucketName)
	if err != nil {
		return err
	}
	if !info.ObjectLockEnabled {
		return ErrObjectLockBucketDisabled
	}

	if err := ValidateRetentionInput(in, time.Now().UnixMilli()); err != nil {
		return err
	}

	_, err = mutateMetadata(bucketName, key, versionID, func(m *storage.Metadatas) error {
		if m.ObjectLockMode == bucket.ObjectLockModeCompliance {
			if in.Mode != bucket.ObjectLockModeCompliance {
				return ErrObjectLockModeDowngrade
			}
			if in.RetainUntilMilli < m.ObjectLockRetainUntilMilli {
				return ErrObjectLockShortenDenied
			}
		}
		if m.ObjectLockMode == bucket.ObjectLockModeGovernance && !bypassGovernance {
			if in.RetainUntilMilli < m.ObjectLockRetainUntilMilli {
				return ErrObjectLockShortenDenied
			}
		}

		m.ObjectLockMode = in.Mode
		m.ObjectLockRetainUntilMilli = in.RetainUntilMilli
		return nil
	})
	return err
}

func GetObjectRetention(bucketName, key, versionID string) (*RetentionInput, error) {
	var m *storage.Metadatas
	var err error
	if versionID != "" {
		m, err = storage.GetObjectVersion(bucketName, key, versionID)
	} else {
		m, err = storage.GetMetadataFromBucket(bucketName, key)
	}
	if err != nil {
		return nil, err
	}
	if m.ObjectLockMode == "" {
		return nil, nil
	}
	return &RetentionInput{Mode: m.ObjectLockMode, RetainUntilMilli: m.ObjectLockRetainUntilMilli}, nil
}

func PutObjectLegalHold(bucketName, key, versionID string, hold bool) error {
	info, err := bucket.GetBucket(bucketName)
	if err != nil {
		return err
	}
	if !info.ObjectLockEnabled {
		return ErrObjectLockBucketDisabled
	}

	_, err = mutateMetadata(bucketName, key, versionID, func(m *storage.Metadatas) error {
		m.ObjectLockLegalHold = hold
		return nil
	})
	return err
}

func GetObjectLegalHold(bucketName, key, versionID string) (bool, error) {
	var m *storage.Metadatas
	var err error
	if versionID != "" {
		m, err = storage.GetObjectVersion(bucketName, key, versionID)
	} else {
		m, err = storage.GetMetadataFromBucket(bucketName, key)
	}
	if err != nil {
		return false, err
	}
	return m.ObjectLockLegalHold, nil
}
