package object

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
)

type GetObjectRequest struct {
	Bucket string
	Key    string
}

type GetObjectResponse struct {
	Key         string
	Filename    string
	ContentType string
	Size        int64
	Reader      io.Reader
}

func GetObject(req *GetObjectRequest) (*GetObjectResponse, error) {
	metadata, err := storage.GetMetadataFromBucket(req.Bucket, req.Key)
	if err != nil {
		return nil, fmt.Errorf("object not found: %w", err)
	}

	reader := &ChunkReader{
		chunkHashes: metadata.ChunkHashes,
		chunksPath:  config.ChunksPath(),
	}

	return &GetObjectResponse{
		Key:         req.Key,
		Filename:    filepath.Base(metadata.Key),
		ContentType: metadata.ContentType,
		Size:        metadata.Size,
		Reader:      reader,
	}, nil
}

func GetMetadata(bucket, key string) (*storage.Metadatas, error) {
	return storage.GetMetadataFromBucket(bucket, key)
}

func NewChunkReaderAt(metadata *storage.Metadatas, startIdx int) *ChunkReader {
	return &ChunkReader{
		chunkHashes: metadata.ChunkHashes,
		chunksPath:  config.ChunksPath(),
		currentIdx:  startIdx,
	}
}

func (cr *ChunkReader) SkipBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, cr, n)
	return err
}

type ChunkReader struct {
	chunkHashes []string
	chunksPath  string
	currentIdx  int
	current     io.ReadCloser
}

func (cr *ChunkReader) Read(p []byte) (n int, err error) {
	for {
		if cr.current == nil {
			if cr.currentIdx >= len(cr.chunkHashes) {
				return 0, io.EOF
			}

			chunkPath := config.ChunkHashToPath(cr.chunkHashes[cr.currentIdx])
			rc, openErr := storage.OpenChunk(chunkPath)
			if openErr != nil {
				return 0, fmt.Errorf("failed to open chunk %s: %w", chunkPath, openErr)
			}
			cr.current = rc
		}

		n, err = cr.current.Read(p)
		if err == io.EOF {
			closeErr := cr.current.Close()
			cr.current = nil
			cr.currentIdx++
			if closeErr != nil {
				return n, closeErr
			}
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (cr *ChunkReader) Close() error {
	if cr.current != nil {
		err := cr.current.Close()
		cr.current = nil
		return err
	}
	return nil
}
