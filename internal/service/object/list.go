package object

import (
	"fmt"
	"strings"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	dbutils "github.com/anhostfr/hangar/pkg/database"
	"github.com/anhostfr/hangar/pkg/pathutil"
)

type ObjectInfo struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	ETag        string `json:"etag"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	CreatedAt   int64  `json:"created_at"`
	ObjectHash  string `json:"object_hash"`
}

type ListObjectsResponse struct {
	Objects []ObjectInfo `json:"objects"`
	Count   int          `json:"count"`
}

// ListObjectsInBucket returns a list of objects in a specific bucket
func ListObjectsInBucket(bucket, prefix string) (*ListObjectsResponse, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var searchPrefix string
	if bucket != "" {
		searchPrefix = fmt.Sprintf("metadata:%s/", bucket)
	} else {
		searchPrefix = "metadata:"
	}

	iter, err := db.NewIteratorWithPrefix([]byte(searchPrefix))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var objects []ObjectInfo

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		objectKey := dbutils.ExtractFilenameFromKey(key)

		// Remove bucket prefix from object key for display
		if bucket != "" {
			if strings.HasPrefix(objectKey, bucket+"/") {
				objectKey = objectKey[len(bucket)+1:]
			}
		}

		if prefix != "" && !strings.HasPrefix(objectKey, prefix) {
			continue
		}

		metadata, err := storage.GetMetadataFromBucket(bucket, objectKey)
		if err != nil {
			continue // Skip corrupted metadata
		}

		objects = append(objects, ObjectInfo{
			Key:         objectKey,
			Filename:    pathutil.ExtractFilename(objectKey),
			ETag:        metadata.ETag,
			Size:        metadata.Size,
			ContentType: metadata.ContentType,
			CreatedAt:   metadata.CreatedAt,
			ObjectHash:  metadata.ObjectHash,
		})
	}

	return &ListObjectsResponse{
		Objects: objects,
		Count:   len(objects),
	}, nil
}

type ListObjectsV2Request struct {
	Bucket            string
	Prefix            string
	Delimiter         string
	ContinuationToken string
	StartAfter        string
	MaxKeys           int
}

type ListObjectsV2Result struct {
	Objects               []ObjectInfo
	CommonPrefixes        []string
	IsTruncated           bool
	NextContinuationToken string
	KeyCount              int
}

func ListObjectsV2(req *ListObjectsV2Request) (*ListObjectsV2Result, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if req.MaxKeys <= 0 {
		req.MaxKeys = 1000
	}
	if req.MaxKeys > 1000 {
		req.MaxKeys = 1000
	}

	searchPrefix := fmt.Sprintf("metadata:%s/", req.Bucket)
	iter, err := db.NewIteratorWithPrefix([]byte(searchPrefix))
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	startKey := ""
	if req.ContinuationToken != "" {
		startKey = req.ContinuationToken
	} else if req.StartAfter != "" {
		startKey = req.StartAfter
	}

	result := &ListObjectsV2Result{}
	seenPrefixes := map[string]struct{}{}
	skipFirst := req.StartAfter != "" && req.ContinuationToken == ""

	for iter.First(); iter.Valid(); iter.Next() {
		fullKey := string(iter.Key())
		objectKey := dbutils.ExtractFilenameFromKey(fullKey)
		if strings.HasPrefix(objectKey, req.Bucket+"/") {
			objectKey = objectKey[len(req.Bucket)+1:]
		}

		if req.Prefix != "" && !strings.HasPrefix(objectKey, req.Prefix) {
			continue
		}

		if startKey != "" {
			if objectKey < startKey {
				continue
			}
			if skipFirst && objectKey == startKey {
				continue
			}
		}

		if req.Delimiter != "" {
			rest := objectKey[len(req.Prefix):]
			if idx := strings.Index(rest, req.Delimiter); idx >= 0 {
				cp := req.Prefix + rest[:idx+len(req.Delimiter)]
				if _, ok := seenPrefixes[cp]; !ok {
					if len(result.Objects)+len(result.CommonPrefixes) >= req.MaxKeys {
						result.IsTruncated = true
						result.NextContinuationToken = objectKey
						break
					}
					seenPrefixes[cp] = struct{}{}
					result.CommonPrefixes = append(result.CommonPrefixes, cp)
				}
				continue
			}
		}

		meta, err := storage.GetMetadataFromBucket(req.Bucket, objectKey)
		if err != nil {
			continue
		}
		if meta.IsDeleteMarker {
			continue
		}

		if len(result.Objects)+len(result.CommonPrefixes) >= req.MaxKeys {
			result.IsTruncated = true
			result.NextContinuationToken = objectKey
			break
		}

		result.Objects = append(result.Objects, ObjectInfo{
			Key:         objectKey,
			Filename:    pathutil.ExtractFilename(objectKey),
			ETag:        meta.ETag,
			Size:        meta.Size,
			ContentType: meta.ContentType,
			CreatedAt:   meta.CreatedAt,
			ObjectHash:  meta.ObjectHash,
		})
	}

	result.KeyCount = len(result.Objects) + len(result.CommonPrefixes)
	return result, nil
}
