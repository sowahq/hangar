package object

import (
	"errors"
	"fmt"
	"strings"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/diskspace"
	"github.com/anhostfr/hangar/internal/storage"
)

var (
	ErrCopySourceNotFound      = errors.New("copy source not found")
	ErrCopyPreconditionFailed  = errors.New("copy source precondition failed")
)

type CopyConditions struct {
	IfMatch           string
	IfNoneMatch       string
	IfModifiedSince   int64
	IfUnmodifiedSince int64
}

type CopyObjectRequest struct {
	SrcBucket         string
	SrcKey            string
	SrcVersion        string
	DstBucket         string
	DstKey            string
	MetadataDirective string
	ContentType       string
	SrcSSE            *SSERequest
	DstSSE            *SSERequest
	Conditions        CopyConditions
}

func CopyObject(req *CopyObjectRequest) (*PutObjectResponse, error) {
	if _, err := bucket.GetBucket(req.DstBucket); err != nil {
		return nil, err
	}

	src, err := loadCopySource(req)
	if err != nil {
		return nil, err
	}

	if err := checkCopyConditions(src, req.Conditions); err != nil {
		return nil, err
	}

	srcEncryptedHint := src.SSEAlgorithm != SSEAlgoNone
	dstWantsEncryptionHint := req.DstSSE != nil && req.DstSSE.Algorithm != SSEAlgoNone

	if srcEncryptedHint || dstWantsEncryptionHint {
		if dsErr := diskspace.Check(src.Size); dsErr != nil {
			return nil, ErrInsufficientStorage
		}
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

	srcEncrypted := src.SSEAlgorithm != SSEAlgoNone
	dstWantsEncryption := req.DstSSE != nil && req.DstSSE.Algorithm != SSEAlgoNone

	if !srcEncrypted && !dstWantsEncryption {
		return fastCopy(req, src, contentType, createdAt, versionID, versioning)
	}

	return reencryptCopy(req, src, contentType, createdAt, versionID, versioning)
}

func fastCopy(req *CopyObjectRequest, src *storage.Metadatas, contentType string, createdAt int64, versionID string, versioning bool) (*PutObjectResponse, error) {
	dst := &storage.Metadatas{
		Key:               req.DstKey,
		ETag:              src.ETag,
		Size:              src.Size,
		ContentType:       contentType,
		CreatedAt:         createdAt,
		ObjectHash:        src.ObjectHash,
		ChunkHashes:       src.ChunkHashes,
		VersionID:         versionID,
		ChecksumAlgorithm: src.ChecksumAlgorithm,
		ChecksumValue:     src.ChecksumValue,
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
		Key:               req.DstKey,
		ETag:              src.ETag,
		Size:              src.Size,
		ContentType:       contentType,
		CreatedAt:         createdAt,
		ObjectHash:        src.ObjectHash,
		VersionID:         versionID,
		ChecksumAlgorithm: src.ChecksumAlgorithm,
		ChecksumValue:     src.ChecksumValue,
	}, nil
}

func reencryptCopy(req *CopyObjectRequest, src *storage.Metadatas, contentType string, createdAt int64, versionID string, versioning bool) (*PutObjectResponse, error) {
	reader, err := newReaderFor(src, req.SrcSSE, 0)
	if err != nil {
		return nil, err
	}

	sse, err := setupSSEWrite(req.DstSSE)
	if err != nil {
		return nil, err
	}

	chunks, fileHash, size, err := storage.ChunkAndHashOpts(reader, config.ChunksPath(), sse.encParams)
	if err != nil {
		return nil, fmt.Errorf("copy chunk: %w", err)
	}

	etag := fmt.Sprintf("%q", fileHash)

	dst := &storage.Metadatas{
		Key:               req.DstKey,
		ETag:              etag,
		Size:              size,
		ContentType:       contentType,
		CreatedAt:         createdAt,
		ObjectHash:        fileHash,
		ChunkHashes:       chunks,
		VersionID:         versionID,
		SSEAlgorithm:      sse.algo,
		SSECustomerKeyMD5: sse.customerKeyMD5,
		SSESalt:           sse.salt,
		SSENoncePrefix:    sse.noncePrefix,
		SSEKeyID:          sse.keyID,
		ChecksumAlgorithm: src.ChecksumAlgorithm,
		ChecksumValue:     src.ChecksumValue,
	}

	if err := storage.IncrementChunkRefs(chunks); err != nil {
		storage.UnmarkChunksPending(chunks)
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	storage.UnmarkChunksPending(chunks)

	if versioning {
		if err := storage.StoreObjectVersion(req.DstBucket, dst); err != nil {
			if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
				return nil, fmt.Errorf("failed to store version (%v) and rollback chunkrefs: %w", err, rbErr)
			}
			return nil, fmt.Errorf("failed to store version: %w", err)
		}
	}

	if err := storage.StoreMetadataInBucket(req.DstBucket, dst); err != nil {
		if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
			return nil, fmt.Errorf("failed to store metadata (%v) and rollback chunkrefs: %w", err, rbErr)
		}
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return &PutObjectResponse{
		Key:            req.DstKey,
		ETag:           etag,
		Size:           size,
		ContentType:    contentType,
		CreatedAt:      createdAt,
		ObjectHash:     fileHash,
		VersionID:      versionID,
		SSEAlgorithm:      sse.algo,
		SSECustomerMD5:    sse.customerKeyMD5,
		ChecksumAlgorithm: src.ChecksumAlgorithm,
		ChecksumValue:     src.ChecksumValue,
	}, nil
}

func checkCopyConditions(src *storage.Metadatas, cond CopyConditions) error {
	etag := stripETagQuotes(src.ETag)

	if cond.IfMatch != "" {
		if !etagAnyMatch(cond.IfMatch, etag) {
			return ErrCopyPreconditionFailed
		}
	}
	if cond.IfNoneMatch != "" {
		if etagAnyMatch(cond.IfNoneMatch, etag) {
			return ErrCopyPreconditionFailed
		}
	}
	if cond.IfUnmodifiedSince > 0 {
		if src.CreatedAt > cond.IfUnmodifiedSince {
			return ErrCopyPreconditionFailed
		}
	}
	if cond.IfModifiedSince > 0 {
		if src.CreatedAt <= cond.IfModifiedSince {
			return ErrCopyPreconditionFailed
		}
	}
	return nil
}

func stripETagQuotes(s string) string {
	s = strings.TrimPrefix(s, "W/")
	s = strings.Trim(s, `"`)
	return s
}

func etagAnyMatch(header, etag string) bool {
	for _, p := range strings.Split(header, ",") {
		v := strings.TrimSpace(p)
		v = strings.TrimPrefix(v, "W/")
		v = strings.Trim(v, `"`)
		if v == "*" || v == etag {
			return true
		}
	}
	return false
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
