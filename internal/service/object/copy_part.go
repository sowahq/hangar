package object

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/diskspace"
	"github.com/anhostfr/hangar/internal/storage"
)

type UploadPartCopyRequest struct {
	DstBucket  string
	DstKey     string
	UploadID   string
	PartNumber int

	SrcBucket  string
	SrcKey     string
	SrcVersion string

	SrcSSE *SSERequest
	DstSSE *SSERequest

	RangeStart int64
	RangeEnd   int64
	HasRange   bool

	Conditions CopyConditions
}

type UploadPartCopyResponse struct {
	PartNumber     int
	ETag           string
	Size           int64
	LastModified   int64
	SrcVersionID   string
}

func UploadPartCopy(req *UploadPartCopyRequest) (*UploadPartCopyResponse, error) {
	if req.PartNumber < MinPartNumber || req.PartNumber > MaxPartNumber {
		return nil, ErrInvalidPartNumber
	}

	if err := diskspace.Check(0); err != nil {
		return nil, ErrInsufficientStorage
	}

	header, err := storage.GetMultipartHeader(req.DstBucket, req.DstKey, req.UploadID)
	if err != nil {
		if errors.Is(err, storage.ErrMultipartNotFound) {
			return nil, ErrMultipartNotFound
		}
		return nil, err
	}

	src, err := loadCopySource(&CopyObjectRequest{
		SrcBucket:  req.SrcBucket,
		SrcKey:     req.SrcKey,
		SrcVersion: req.SrcVersion,
	})
	if err != nil {
		return nil, err
	}

	if err := checkCopyConditions(src, req.Conditions); err != nil {
		return nil, err
	}

	start, length, rangeErr := resolveCopyRange(src.Size, req.HasRange, req.RangeStart, req.RangeEnd)
	if rangeErr != nil {
		return nil, rangeErr
	}

	chunkSize := int64(config.ChunkSize())
	startChunk := int(start / chunkSize)
	skip := start % chunkSize

	srcReader, err := newReaderFor(src, req.SrcSSE, startChunk)
	if err != nil {
		return nil, err
	}

	if skip > 0 {
		if err := srcReader.SkipBytes(skip); err != nil {
			_ = srcReader.Close()
			return nil, fmt.Errorf("seek copy source: %w", err)
		}
	}

	var reader io.Reader = io.LimitReader(srcReader, length)

	encParams, encErr := uploadPartEncryptParams(header, &UploadPartRequest{
		PartNumber: req.PartNumber,
		SSE:        req.DstSSE,
	})
	if encErr != nil {
		_ = srcReader.Close()
		return nil, encErr
	}

	chunks, partHash, size, chErr := storage.ChunkAndHashOpts(reader, config.ChunksPath(), encParams)
	_ = srcReader.Close()
	if chErr != nil {
		return nil, fmt.Errorf("chunk part copy: %w", chErr)
	}

	if err := storage.IncrementChunkRefs(chunks); err != nil {
		storage.UnmarkChunksPending(chunks)
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	storage.UnmarkChunksPending(chunks)

	etag := fmt.Sprintf("%q", partHash)

	if existing, gErr := storage.GetMultipartPart(req.DstBucket, req.DstKey, req.UploadID, req.PartNumber); gErr == nil {
		if dErr := storage.DecrementChunkRefs(existing.ChunkHashes); dErr != nil {
			return nil, fmt.Errorf("failed to decrement chunk refs of replaced part: %w", dErr)
		}
	}

	part := &storage.MultipartPart{
		PartNumber:  req.PartNumber,
		Size:        size,
		ETag:        etag,
		ChunkHashes: chunks,
		UploadedAt:  time.Now().UnixMilli(),
	}

	if err := storage.StoreMultipartPart(req.DstBucket, req.DstKey, req.UploadID, part); err != nil {
		if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
			return nil, fmt.Errorf("failed to store part (%v) and rollback chunkrefs: %w", err, rbErr)
		}
		return nil, err
	}

	return &UploadPartCopyResponse{
		PartNumber:   req.PartNumber,
		ETag:         etag,
		Size:         size,
		LastModified: src.CreatedAt,
		SrcVersionID: src.VersionID,
	}, nil
}

func resolveCopyRange(srcSize int64, hasRange bool, start, end int64) (int64, int64, error) {
	if !hasRange {
		return 0, srcSize, nil
	}
	if start < 0 || end < start || end >= srcSize {
		return 0, 0, fmt.Errorf("invalid copy-source range")
	}
	return start, end - start + 1, nil
}
