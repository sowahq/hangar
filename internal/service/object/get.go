package object

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/crypto"
)

type GetObjectRequest struct {
	Bucket    string
	Key       string
	VersionID string
	SSE       *SSERequest
}

type GetObjectResponse struct {
	Key         string
	Filename    string
	ContentType string
	Size        int64
	Reader      io.Reader
}

func GetObject(req *GetObjectRequest) (*GetObjectResponse, error) {
	var metadata *storage.Metadatas
	var err error
	if req.VersionID != "" {
		metadata, err = storage.GetObjectVersion(req.Bucket, req.Key, req.VersionID)
	} else {
		metadata, err = storage.GetMetadataFromBucket(req.Bucket, req.Key)
	}
	if err != nil {
		return nil, fmt.Errorf("object not found: %w", err)
	}
	if metadata.IsDeleteMarker {
		return nil, fmt.Errorf("object not found")
	}

	reader, err := newReaderFor(metadata, req.SSE, 0)
	if err != nil {
		return nil, err
	}

	return &GetObjectResponse{
		Key:         req.Key,
		Filename:    filepath.Base(metadata.Key),
		ContentType: metadata.ContentType,
		Size:        metadata.Size,
		Reader:      reader,
	}, nil
}

func newReaderFor(m *storage.Metadatas, sse *SSERequest, startIdx int) (*ChunkReader, error) {
	cr := &ChunkReader{
		metadata:    m,
		chunkHashes: m.ChunkHashes,
		chunksPath:  config.ChunksPath(),
		currentIdx:  startIdx,
	}

	key, err := ResolveReadKey(m, sse)
	if err != nil {
		return nil, err
	}
	cr.key = key

	return cr, nil
}

func GetMetadata(bucket, key string) (*storage.Metadatas, error) {
	return storage.GetMetadataFromBucket(bucket, key)
}

func GetVersionMetadata(bucket, key, versionID string) (*storage.Metadatas, error) {
	return storage.GetObjectVersion(bucket, key, versionID)
}

type ObjectVersionInfo struct {
	VersionID      string `json:"version_id"`
	Size           int64  `json:"size"`
	ETag           string `json:"etag"`
	ContentType    string `json:"content_type"`
	CreatedAt      int64  `json:"created_at"`
	IsDeleteMarker bool   `json:"is_delete_marker"`
	IsCurrent      bool   `json:"is_current"`
}

type ListVersionsResponse struct {
	Key      string              `json:"key"`
	Versions []ObjectVersionInfo `json:"versions"`
	Count    int                 `json:"count"`
}

func ListVersions(bucket, key string) (*ListVersionsResponse, error) {
	versions, err := storage.ListObjectVersions(bucket, key)
	if err != nil {
		return nil, err
	}
	cur, _ := storage.GetMetadataFromBucket(bucket, key)
	var curVid string
	if cur != nil {
		curVid = cur.VersionID
	}
	out := make([]ObjectVersionInfo, 0, len(versions))
	for _, v := range versions {
		out = append(out, ObjectVersionInfo{
			VersionID:      v.VersionID,
			Size:           v.Size,
			ETag:           v.ETag,
			ContentType:    v.ContentType,
			CreatedAt:      v.CreatedAt,
			IsDeleteMarker: v.IsDeleteMarker,
			IsCurrent:      curVid != "" && v.VersionID == curVid,
		})
	}
	return &ListVersionsResponse{Key: key, Versions: out, Count: len(out)}, nil
}

func NewChunkReaderAt(metadata *storage.Metadatas, startIdx int) *ChunkReader {
	return &ChunkReader{
		metadata:    metadata,
		chunkHashes: metadata.ChunkHashes,
		chunksPath:  config.ChunksPath(),
		currentIdx:  startIdx,
	}
}

func NewChunkReaderAtWithSSE(metadata *storage.Metadatas, startIdx int, sse *SSERequest) (*ChunkReader, error) {
	return newReaderFor(metadata, sse, startIdx)
}

func (cr *ChunkReader) SkipBytes(n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, cr, n)
	return err
}

type ChunkReader struct {
	metadata    *storage.Metadatas
	chunkHashes []string
	chunksPath  string
	currentIdx  int
	current     io.ReadCloser
	key         []byte
}

func (cr *ChunkReader) Read(p []byte) (n int, err error) {
	for {
		if cr.current == nil {
			if cr.currentIdx >= len(cr.chunkHashes) {
				return 0, io.EOF
			}

			hash := cr.chunkHashes[cr.currentIdx]

			var rc io.ReadCloser
			var openErr error
			if cr.key != nil {
				partNum, localIdx := chunkPartLookup(cr.metadata, cr.currentIdx)
				nonce, nErr := crypto.ChunkNonce(cr.metadata.SSENoncePrefix, storage.ChunkNonceIdx(partNum, localIdx))
				if nErr != nil {
					return 0, fmt.Errorf("chunk nonce: %w", nErr)
				}
				rc, openErr = storage.OpenChunkEncrypted(hash, cr.key, nonce)
			} else {
				rc, openErr = storage.OpenChunk(hash)
			}
			if openErr != nil {
				return 0, fmt.Errorf("failed to open chunk %s: %w", hash, openErr)
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
