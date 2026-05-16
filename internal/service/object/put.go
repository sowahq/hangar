package object

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/diskspace"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/pathutil"
)

var (
	ErrQuotaExceeded       = errors.New("quota exceeded")
	ErrLengthRequired      = errors.New("content-length required when quota enabled")
	ErrInsufficientStorage = errors.New("insufficient storage")
)

type PutObjectRequest struct {
	Bucket        string
	Key           string
	Body          io.Reader
	ContentLength int64
	ContentType   string
	SSE           *SSERequest
}

type PutObjectResponse struct {
	Key            string `json:"key"`
	Filename       string `json:"filename"`
	ETag           string `json:"etag"`
	Size           int64  `json:"size"`
	ContentType    string `json:"content_type"`
	CreatedAt      int64  `json:"created_at"`
	ObjectHash     string `json:"object_hash"`
	VersionID      string `json:"version_id,omitempty"`
	SSEAlgorithm   string `json:"sse_algorithm,omitempty"`
	SSECustomerMD5 string `json:"sse_customer_md5,omitempty"`
}

var versionSeq uint64

func newVersionID() string {
	ns := time.Now().UnixNano()
	seq := atomic.AddUint64(&versionSeq, 1)
	return strconv.FormatInt(ns, 10) + "-" + strconv.FormatUint(seq, 10)
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}

func PutObject(req *PutObjectRequest) (*PutObjectResponse, error) {
	if err := diskspace.Check(req.ContentLength); err != nil {
		return nil, ErrInsufficientStorage
	}

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

	var contentType string
	var fullReader io.Reader
	if req.ContentType != "" {
		contentType = req.ContentType
		fullReader = req.Body
	} else {
		probeBuf := make([]byte, 4096)
		n, err := io.ReadFull(req.Body, probeBuf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("failed to read content for probing: %w", err)
		}
		contentType = http.DetectContentType(probeBuf[:n])
		fullReader = io.MultiReader(bytes.NewReader(probeBuf[:n]), req.Body)
	}

	sse, err := setupSSEWrite(req.SSE)
	if err != nil {
		return nil, err
	}

	chunks, fileHash, size, err := storage.ChunkAndHashOpts(fullReader, config.ChunksPath(), sse.encParams)
	if err != nil {
		return nil, fmt.Errorf("failed to chunk and hash object: %w", err)
	}

	etag := fmt.Sprintf("%q", fileHash)
	createdAt := time.Now().UnixMilli()

	versioning := info != nil && info.VersioningEnabled
	var versionID string
	if versioning {
		versionID = newVersionID()
	}

	metadata := &storage.Metadatas{
		Key:               req.Key,
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
	}

	if err := storage.IncrementChunkRefs(chunks); err != nil {
		storage.UnmarkChunksPending(chunks)
		return nil, fmt.Errorf("failed to increment chunk refs: %w", err)
	}

	storage.UnmarkChunksPending(chunks)

	if versioning {
		if err := storage.StoreObjectVersion(req.Bucket, metadata); err != nil {
			if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
				return nil, fmt.Errorf("failed to store version (%v) and to rollback chunkrefs: %w", err, rbErr)
			}
			return nil, fmt.Errorf("failed to store version: %w", err)
		}
	}

	if err := storage.StoreMetadataInBucket(req.Bucket, metadata); err != nil {
		if rbErr := storage.DecrementChunkRefs(chunks); rbErr != nil {
			return nil, fmt.Errorf("failed to store metadata (%v) and to rollback chunkrefs: %w", err, rbErr)
		}
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return &PutObjectResponse{
		Key:            req.Key,
		Filename:       pathutil.ExtractFilename(req.Key),
		ETag:           etag,
		Size:           size,
		ContentType:    contentType,
		CreatedAt:      createdAt,
		ObjectHash:     fileHash,
		VersionID:      versionID,
		SSEAlgorithm:   sse.algo,
		SSECustomerMD5: sse.customerKeyMD5,
	}, nil
}
