package object

import (
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
)

func loadObjectForTagging(bucketName, key, versionID string) (*storage.Metadatas, error) {
	if versionID != "" {
		m, err := storage.GetObjectVersion(bucketName, key, versionID)
		if err != nil {
			return nil, ErrObjectNotFound
		}
		return m, nil
	}
	m, err := storage.GetMetadataFromBucket(bucketName, key)
	if err != nil || m == nil {
		return nil, ErrObjectNotFound
	}
	if m.IsDeleteMarker {
		return nil, ErrObjectNotFound
	}
	return m, nil
}

func saveObjectAfterTagging(bucketName string, m *storage.Metadatas, versionID string) error {
	if versionID != "" {
		return storage.StoreObjectVersion(bucketName, m)
	}
	return storage.StoreMetadataInBucket(bucketName, m)
}

func PutObjectTagging(bucketName, key, versionID string, tags []storage.Tag) error {
	if err := bucket.ValidateTags(tags); err != nil {
		return err
	}
	m, err := loadObjectForTagging(bucketName, key, versionID)
	if err != nil {
		return err
	}
	m.Tags = tags
	if err := saveObjectAfterTagging(bucketName, m, versionID); err != nil {
		return fmt.Errorf("save tags: %w", err)
	}
	return nil
}

func GetObjectTagging(bucketName, key, versionID string) ([]storage.Tag, error) {
	m, err := loadObjectForTagging(bucketName, key, versionID)
	if err != nil {
		return nil, err
	}
	return m.Tags, nil
}

func DeleteObjectTagging(bucketName, key, versionID string) error {
	m, err := loadObjectForTagging(bucketName, key, versionID)
	if err != nil {
		return err
	}
	m.Tags = nil
	return saveObjectAfterTagging(bucketName, m, versionID)
}
