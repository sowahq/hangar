package object

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/pathutil"
	"github.com/zeebo/blake3"
)

const (
	MinPartNumber = 1
	MaxPartNumber = 10000
)

var (
	ErrInvalidPartNumber  = errors.New("invalid part number")
	ErrMultipartNotFound  = errors.New("multipart upload not found")
	ErrNoPartsToComplete  = errors.New("no parts to complete")
	ErrPartMissing        = errors.New("part missing")
	ErrCompleteQuotaFail  = errors.New("quota exceeded on complete")
)

type InitiateMultipartRequest struct {
	Bucket string
	Key    string
}

type InitiateMultipartResponse struct {
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	UploadID string `json:"upload_id"`
}

func newUploadID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func InitiateMultipart(req *InitiateMultipartRequest) (*InitiateMultipartResponse, error) {
	if _, err := bucket.GetBucket(req.Bucket); err != nil {
		return nil, err
	}
	uploadID, err := newUploadID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate upload id: %w", err)
	}
	h := &storage.MultipartHeader{
		UploadID:  uploadID,
		Bucket:    req.Bucket,
		Key:       req.Key,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := storage.StoreMultipartHeader(h); err != nil {
		return nil, err
	}
	return &InitiateMultipartResponse{Bucket: req.Bucket, Key: req.Key, UploadID: uploadID}, nil
}

type UploadPartRequest struct {
	Bucket     string
	Key        string
	UploadID   string
	PartNumber int
	Body       io.Reader
}

type UploadPartResponse struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

func UploadPart(req *UploadPartRequest) (*UploadPartResponse, error) {
	if req.PartNumber < MinPartNumber || req.PartNumber > MaxPartNumber {
		return nil, ErrInvalidPartNumber
	}
	if _, err := storage.GetMultipartHeader(req.Bucket, req.Key, req.UploadID); err != nil {
		if errors.Is(err, storage.ErrMultipartNotFound) {
			return nil, ErrMultipartNotFound
		}
		return nil, err
	}

	chunks, partHash, size, err := storage.ChunkAndHash(req.Body, config.ChunksPath())
	if err != nil {
		return nil, fmt.Errorf("failed to chunk part: %w", err)
	}

	if err := storage.IncrementChunkRefs(chunks); err != nil {
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	etag := fmt.Sprintf("%q", partHash)

	if existing, err := storage.GetMultipartPart(req.Bucket, req.Key, req.UploadID, req.PartNumber); err == nil {
		if err := storage.DecrementChunkRefs(existing.ChunkHashes); err != nil {
			return nil, fmt.Errorf("failed to decrement chunk refs of replaced part: %w", err)
		}
	}

	part := &storage.MultipartPart{
		PartNumber:  req.PartNumber,
		Size:        size,
		ETag:        etag,
		ChunkHashes: chunks,
		UploadedAt:  time.Now().UnixMilli(),
	}
	if err := storage.StoreMultipartPart(req.Bucket, req.Key, req.UploadID, part); err != nil {
		if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
			return nil, fmt.Errorf("failed to store part (%v) and rollback chunkrefs: %w", err, rbErr)
		}
		return nil, err
	}

	return &UploadPartResponse{PartNumber: req.PartNumber, ETag: etag, Size: size}, nil
}

type CompleteMultipartRequest struct {
	Bucket   string
	Key      string
	UploadID string
	Parts    []int
}

func CompleteMultipart(req *CompleteMultipartRequest) (*PutObjectResponse, error) {
	if _, err := storage.GetMultipartHeader(req.Bucket, req.Key, req.UploadID); err != nil {
		if errors.Is(err, storage.ErrMultipartNotFound) {
			return nil, ErrMultipartNotFound
		}
		return nil, err
	}

	available, err := storage.ListMultipartParts(req.Bucket, req.Key, req.UploadID)
	if err != nil {
		return nil, err
	}
	if len(available) == 0 {
		return nil, ErrNoPartsToComplete
	}

	byNum := make(map[int]*storage.MultipartPart, len(available))
	for _, p := range available {
		byNum[p.PartNumber] = p
	}

	var ordered []*storage.MultipartPart
	if len(req.Parts) == 0 {
		nums := make([]int, 0, len(available))
		for _, p := range available {
			nums = append(nums, p.PartNumber)
		}
		sort.Ints(nums)
		for _, n := range nums {
			ordered = append(ordered, byNum[n])
		}
	} else {
		for _, n := range req.Parts {
			p, ok := byNum[n]
			if !ok {
				return nil, fmt.Errorf("%w: %d", ErrPartMissing, n)
			}
			ordered = append(ordered, p)
		}
	}

	var totalSize int64
	var allChunks []string
	etagHasher := blake3.New()
	for _, p := range ordered {
		totalSize += p.Size
		allChunks = append(allChunks, p.ChunkHashes...)
		etagHasher.Write([]byte(p.ETag))
	}

	info, _ := bucket.GetBucket(req.Bucket)
	quotaEnabled := info != nil && (info.MaxBytes > 0 || info.MaxObjects > 0)
	if quotaEnabled {
		curBytes, curObjects, usageErr := bucket.GetUsage(req.Bucket)
		if usageErr != nil {
			return nil, fmt.Errorf("failed to get usage: %w", usageErr)
		}
		if info.MaxBytes > 0 && curBytes+totalSize > info.MaxBytes {
			return nil, ErrCompleteQuotaFail
		}
		if info.MaxObjects > 0 && curObjects+1 > info.MaxObjects {
			return nil, ErrCompleteQuotaFail
		}
	}

	combinedHash := fmt.Sprintf("%x", etagHasher.Sum(nil))
	finalETag := fmt.Sprintf("\"%s-%d\"", combinedHash, len(ordered))
	createdAt := time.Now().UnixMilli()

	versioning := info != nil && info.VersioningEnabled
	var versionID string
	if versioning {
		versionID = newVersionID()
	}

	metadata := &storage.Metadatas{
		Key:         req.Key,
		ETag:        finalETag,
		Size:        totalSize,
		ContentType: "application/octet-stream",
		CreatedAt:   createdAt,
		ObjectHash:  combinedHash,
		ChunkHashes: allChunks,
		VersionID:   versionID,
	}

	if versioning {
		if err := storage.StoreObjectVersion(req.Bucket, metadata); err != nil {
			return nil, fmt.Errorf("failed to store version: %w", err)
		}
	}
	if err := storage.StoreMetadataInBucket(req.Bucket, metadata); err != nil {
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	if _, err := storage.DeleteMultipart(req.Bucket, req.Key, req.UploadID); err != nil {
		return nil, fmt.Errorf("failed to cleanup multipart state: %w", err)
	}

	return &PutObjectResponse{
		Key:         req.Key,
		Filename:    pathutil.ExtractFilename(req.Key),
		ETag:        finalETag,
		Size:        totalSize,
		ContentType: metadata.ContentType,
		CreatedAt:   createdAt,
		ObjectHash:  combinedHash,
		VersionID:   versionID,
	}, nil
}

type AbortMultipartRequest struct {
	Bucket   string
	Key      string
	UploadID string
}

func AbortMultipart(req *AbortMultipartRequest) error {
	if _, err := storage.GetMultipartHeader(req.Bucket, req.Key, req.UploadID); err != nil {
		if errors.Is(err, storage.ErrMultipartNotFound) {
			return ErrMultipartNotFound
		}
		return err
	}
	parts, err := storage.ListMultipartParts(req.Bucket, req.Key, req.UploadID)
	if err != nil {
		return err
	}
	var allChunks []string
	for _, p := range parts {
		allChunks = append(allChunks, p.ChunkHashes...)
	}
	if len(allChunks) > 0 {
		if err := storage.DecrementChunkRefs(allChunks); err != nil {
			return fmt.Errorf("failed to decrement chunk refs: %w", err)
		}
	}
	if _, err := storage.DeleteMultipart(req.Bucket, req.Key, req.UploadID); err != nil {
		return err
	}
	return nil
}

type ListMultipartPartsResponse struct {
	Bucket   string                  `json:"bucket"`
	Key      string                  `json:"key"`
	UploadID string                  `json:"upload_id"`
	Parts    []storage.MultipartPart `json:"parts"`
	Count    int                     `json:"count"`
}

func ListPartsService(bucket, key, uploadID string) (*ListMultipartPartsResponse, error) {
	if _, err := storage.GetMultipartHeader(bucket, key, uploadID); err != nil {
		if errors.Is(err, storage.ErrMultipartNotFound) {
			return nil, ErrMultipartNotFound
		}
		return nil, err
	}
	parts, err := storage.ListMultipartParts(bucket, key, uploadID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.MultipartPart, 0, len(parts))
	for _, p := range parts {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PartNumber < out[j].PartNumber })
	return &ListMultipartPartsResponse{Bucket: bucket, Key: key, UploadID: uploadID, Parts: out, Count: len(out)}, nil
}
