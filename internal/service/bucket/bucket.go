package bucket

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/object"
)

// Bucket types and requests
type BucketInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Public    bool      `json:"public"`
	Objects   int64     `json:"objects"`
	Size      int64     `json:"size"`
}

type CreateBucketRequest struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
}

type CreateBucketResponse struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Public    bool      `json:"public"`
}

type ListBucketsResponse struct {
	Buckets []BucketInfo `json:"buckets"`
	Count   int          `json:"count"`
}

type DeleteBucketRequest struct {
	Name  string `json:"name"`
	Force bool   `json:"force"`
}

// CreateBucket creates a new bucket
func CreateBucket(req *CreateBucketRequest) (*CreateBucketResponse, error) {
	if err := BucketName(req.Name); err != nil {
		return nil, err
	}

	bucketPath := filepath.Join(config.DataPath(), req.Name)
	
	// Check if bucket already exists
	if _, err := os.Stat(bucketPath); err == nil {
		return nil, fmt.Errorf("bucket already exists: %s", req.Name)
	}

	// Create bucket directory
	if err := os.MkdirAll(bucketPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bucket directory: %w", err)
	}

	now := time.Now()
	
	return &CreateBucketResponse{
		Name:      req.Name,
		CreatedAt: now,
		Public:    req.Public,
	}, nil
}

// GetBucket gets bucket information
func GetBucket(name string) (*BucketInfo, error) {
	if err := BucketName(name); err != nil {
		return nil, err
	}

	bucketPath := filepath.Join(config.DataPath(), name)
	
	// Check if bucket exists
	info, err := os.Stat(bucketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bucket not found: %s", name)
		}
		return nil, fmt.Errorf("failed to access bucket: %w", err)
	}

	// Count objects and calculate size
	objects, size, err := countBucketObjects(name)
	if err != nil {
		objects = 0
		size = 0
	}

	return &BucketInfo{
		Name:      name,
		CreatedAt: info.ModTime(),
		Public:    false, // TODO: implement public bucket support
		Objects:   objects,
		Size:      size,
	}, nil
}

// ListBuckets lists all buckets
func ListBuckets() (*ListBucketsResponse, error) {
	dataPath := config.DataPath()
	
	entries, err := os.ReadDir(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	var buckets []BucketInfo
	for _, entry := range entries {
		if entry.IsDir() {
			bucketInfo, err := GetBucket(entry.Name())
			if err != nil {
				continue // Skip invalid buckets
			}
			buckets = append(buckets, *bucketInfo)
		}
	}

	return &ListBucketsResponse{
		Buckets: buckets,
		Count:   len(buckets),
	}, nil
}

// DeleteBucket deletes a bucket
func DeleteBucket(req *DeleteBucketRequest) error {
	if err := BucketName(req.Name); err != nil {
		return err
	}

	bucketPath := filepath.Join(config.DataPath(), req.Name)
	
	// Check if bucket exists
	if _, err := os.Stat(bucketPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("bucket not found: %s", req.Name)
		}
		return fmt.Errorf("failed to access bucket: %w", err)
	}

	// Check if bucket is empty (unless force is specified)
	if !req.Force {
		objects, _, err := countBucketObjects(req.Name)
		if err != nil {
			return fmt.Errorf("failed to check bucket contents: %w", err)
		}
		if objects > 0 {
			return fmt.Errorf("bucket is not empty (use force=true to delete anyway)")
		}
	}

	// Remove bucket directory
	if err := os.RemoveAll(bucketPath); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	return nil
}

// countBucketObjects counts objects and calculates total size in a bucket
func countBucketObjects(bucketName string) (int64, int64, error) {
	response, err := object.ListObjectsInBucket(bucketName, "")
	if err != nil {
		return 0, 0, err
	}

	var totalSize int64
	for _, obj := range response.Objects {
		totalSize += obj.Size
	}

	return int64(response.Count), totalSize, nil
}