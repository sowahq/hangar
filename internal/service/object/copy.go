package object

import (
	"errors"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
)

var ErrCopySourceNotFound = errors.New("copy source not found")

type CopyObjectRequest struct {
	SrcBucket         string
	SrcKey            string
	SrcVersion        string
	DstBucket         string
	DstKey            string
	MetadataDirective string
	ContentType       string
}

func CopyObject(req *CopyObjectRequest) (*PutObjectResponse, error) {
	if _, err := bucket.GetBucket(req.DstBucket); err != nil {
		return nil, err
	}

	src, err := loadCopySource(req)
	if err != nil {
		return nil, err
	}

	info, _ := bucket.GetBucket(req.DstBucket)

	if err := checkCopyQuota(info, req.DstBucket, src.Size); err != nil {
		return nil, err
	}

	contentType := src.ContentType
	if req.MetadataDirective == "REPLACE" && req.ContentType != "" {
		contentType = req.ContentType
	}

	createdAt := nowMillis()

	versioning := info != nil && info.VersioningEnabled
	var versionID string
	if versioning {
		versionID = newVersionID()
	}

	dst := &storage.Metadatas{
		Key:         req.DstKey,
		ETag:        src.ETag,
		Size:        src.Size,
		ContentType: contentType,
		CreatedAt:   createdAt,
		ObjectHash:  src.ObjectHash,
		ChunkHashes: src.ChunkHashes,
		VersionID:   versionID,
	}

	if err := storage.IncrementChunkRefs(dst.ChunkHashes); err != nil {
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	if versioning {
		if err := storage.StoreObjectVersion(req.DstBucket, dst); err != nil {
			if rbErr := storage.DecrementChunkRefs(dst.ChunkHashes); rbErr != nil {
				return nil, fmt.Errorf("failed to store version (%v) and rollback chunkrefs: %w", err, rbErr)
			}
			return nil, fmt.Errorf("failed to store version: %w", err)
		}
	}

	if err := storage.StoreMetadataInBucket(req.DstBucket, dst); err != nil {
		if rbErr := storage.DecrementChunkRefs(dst.ChunkHashes); rbErr != nil {
			return nil, fmt.Errorf("failed to store metadata (%v) and rollback chunkrefs: %w", err, rbErr)
		}
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return &PutObjectResponse{
		Key:         req.DstKey,
		ETag:        src.ETag,
		Size:        src.Size,
		ContentType: contentType,
		CreatedAt:   createdAt,
		ObjectHash:  src.ObjectHash,
		VersionID:   versionID,
	}, nil
}

func loadCopySource(req *CopyObjectRequest) (*storage.Metadatas, error) {
	var src *storage.Metadatas
	var err error

	if req.SrcVersion != "" {
		src, err = storage.GetObjectVersion(req.SrcBucket, req.SrcKey, req.SrcVersion)
	} else {
		src, err = storage.GetMetadataFromBucket(req.SrcBucket, req.SrcKey)
	}
	if err != nil {
		return nil, ErrCopySourceNotFound
	}

	if src.IsDeleteMarker {
		return nil, ErrCopySourceNotFound
	}

	return src, nil
}

func checkCopyQuota(info *bucket.BucketInfo, dstBucket string, size int64) error {
	if info == nil || (info.MaxBytes == 0 && info.MaxObjects == 0) {
		return nil
	}

	curBytes, curObjects, usageErr := bucket.GetUsage(dstBucket)
	if usageErr != nil {
		return fmt.Errorf("failed to get usage: %w", usageErr)
	}

	if info.MaxBytes > 0 && curBytes+size > info.MaxBytes {
		return ErrQuotaExceeded
	}

	if info.MaxObjects > 0 && curObjects+1 > info.MaxObjects {
		return ErrQuotaExceeded
	}

	return nil
}
