package object

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zeebo/blake3"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/internal/utils/path"
)

type PutObjectRequest struct {
	Bucket string
	Key    string
	Body   io.Reader
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

// PutObject handles the core logic for uploading and storing an object
func PutObject(req *PutObjectRequest) (*PutObjectResponse, error) {

	probeSize := 4096 // 4KB
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

	metadata := &storage.Metadatas{
		Filename:    req.Key,
		ETag:        fmt.Sprintf("%x", blake3.Sum256([]byte(req.Key))),
		Size:        size,
		ContentType: contentType,
		CreatedAt:   time.Now().UnixMilli(),
		ObjectHash:  fileHash,
		ChunkHashes: chunks,
	}

	if err := storage.StoreMetadataInBucket(req.Bucket, metadata); err != nil {
		return nil, fmt.Errorf("failed to store metadata: %w", err)
	}

	return &PutObjectResponse{
		Key:         req.Key,
		Filename:    path.ExtractFilename(req.Key),
		ETag:        fmt.Sprintf("%x", blake3.Sum256([]byte(req.Key))),
		Size:        size,
		ContentType: contentType,
		CreatedAt:   time.Now().UnixMilli(),
		ObjectHash:  fileHash,
	}, nil
}
