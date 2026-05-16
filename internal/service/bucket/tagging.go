package bucket

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

var ErrTaggingNotFound = errors.New("tagging configuration not found")

const (
	MaxTags         = 10
	MaxTagKeyLen    = 128
	MaxTagValueLen  = 256
)

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func taggingKey(bucket string) []byte {
	return []byte(fmt.Sprintf("tagging:%s", bucket))
}

func ValidateTags(tags []Tag) error {
	if len(tags) > MaxTags {
		return fmt.Errorf("too many tags: max %d", MaxTags)
	}
	seen := map[string]bool{}
	for _, t := range tags {
		if t.Key == "" || len(t.Key) > MaxTagKeyLen {
			return fmt.Errorf("invalid tag key length")
		}
		if len(t.Value) > MaxTagValueLen {
			return fmt.Errorf("invalid tag value length")
		}
		if seen[t.Key] {
			return fmt.Errorf("duplicate tag key: %s", t.Key)
		}
		seen[t.Key] = true
	}
	return nil
}

func PutBucketTagging(bucket string, tags []Tag) error {
	if _, err := GetBucket(bucket); err != nil {
		return err
	}
	if err := ValidateTags(tags); err != nil {
		return err
	}

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	data, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal tagging: %w", err)
	}

	return db.Put(taggingKey(bucket), data)
}

func GetBucketTagging(bucket string) ([]Tag, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(taggingKey(bucket))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrTaggingNotFound
		}
		return nil, fmt.Errorf("get tagging: %w", err)
	}

	var tags []Tag
	if err := json.Unmarshal(data, &tags); err != nil {
		return nil, fmt.Errorf("unmarshal tagging: %w", err)
	}

	return tags, nil
}

func DeleteBucketTagging(bucket string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	return db.Delete(taggingKey(bucket))
}
