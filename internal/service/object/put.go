package object

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/pathutil"
)

var (
	ErrQuotaExceeded    = errors.New("quota exceeded")
	ErrLengthRequired   = errors.New("content-length required when quota enabled")
)

type PutObjectRequest struct {
	Bucket        string
	Key           string
	Body          io.Reader
	ContentLength int64
}

type PutObjectResponse struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	ETag        string `json:"etag"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	CreatedAt   int64  `json:"created_at"`
	ObjectHash  string `json:"object_hash"`
}

func PutObject(req *PutObjectRequest) (*PutObjectResponse, error) {
	info, _ := bucket.GetBucket(req.Bucket)
	quotaEnabled := info != nil && (info.MaxBytes > 0 || info.MaxObjects > 0)
	if quotaEnabled {
		if req.ContentLength <= 0 {
			return nil, ErrLengthRequired
		}
		curBytes, curObjects, usageErr := bucket.GetUsage(req.Bucket)
		if usageErr != nil {
			return nil, fmt.Errorf("failed to get usage: %w", usageErr)
		}
		if info.MaxBytes > 0 && curBytes+req.ContentLength > info.MaxBytes {
			return nil, ErrQuotaExceeded
		}
		if info.MaxObjects > 0 && curObjects+1 > info.MaxObjects {
			return nil, ErrQuotaExceeded
		}
	}

	probeSize := 4096
	probeBuf := make([]byte, probeSize)
	n, err := io.ReadFull(req.Body, probeBuf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read content for probing: %w", err)
	}

	contentType := http.DetectContentType(probeBuf[:n])
	fullReader := io.MultiReader(bytes.NewReader(probeBuf[:n]), req.Body)

	chunks, fileHash, size, err := storage.ChunkAndHash(fullReader, config.ChunksPath())
	if err != nil {
		return nil, fmt.Errorf("failed to chunk and hash object: %w", err)
	}

	etag := fmt.Sprintf("%q", fileHash)
	createdAt := time.Now().UnixMilli()

	metadata := &storage.Metadatas{
		Key:         req.Key,
		ETag:        etag,
		Size:        size,
		ContentType: contentType,
		CreatedAt:   createdAt,
		ObjectHash:  fileHash,
		ChunkHashes: chunks,
	}

	if err := storage.IncrementChunkRefs(chunks); err != nil {
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	if err := storage.StoreMetadataInBucket(req.Bucket, metadata); err != nil {
		if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
			return nil, fmt.Errorf("failed to store metadata (%v) and to rollback chunkrefs: %w", err, rbErr)
		}
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return &PutObjectResponse{
		Key:         req.Key,
		Filename:    pathutil.ExtractFilename(req.Key),
		ETag:        etag,
		Size:        size,
		ContentType: contentType,
		CreatedAt:   createdAt,
		ObjectHash:  fileHash,
	}, nil
}
