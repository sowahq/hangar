package object

import (
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/internal/utils/path"
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

// GetObject retrieves an object by its key and reconstructs it from chunks
func GetObject(req *GetObjectRequest) (*GetObjectResponse, error) {
	metadata, err := storage.GetMetadataFromBucket(req.Bucket, req.Key)
	if err != nil {
		return nil, fmt.Errorf("object not found: %w", err)
	}

	reader := &ChunkReader{
		chunkHashes: metadata.ChunkHashes,
		chunksPath:  config.ChunksPath(),
		currentIdx:  0,
	}

	return &GetObjectResponse{
		Key:         req.Key,
		Filename:    path.ExtractFilename(req.Key),
		ContentType: metadata.ContentType,
		Size:        metadata.Size,
		Reader:      reader,
	}, nil
}

// ChunkReader implements io.Reader to reconstruct a file from chunks
type ChunkReader struct {
	chunkHashes  []string
	chunksPath   string
	currentIdx   int
	currentFile  io.ReadCloser
	decompressor io.Reader     // Current chunk reader
	decoder      *zstd.Decoder // Reusable decoder
}

func (cr *ChunkReader) Read(p []byte) (n int, err error) {
	for {
		if cr.decompressor == nil {
			if cr.currentIdx >= len(cr.chunkHashes) {
				return 0, io.EOF
			}

			chunkPath := config.ChunkHashToPath(cr.chunkHashes[cr.currentIdx])

			chunkData, err := os.ReadFile(chunkPath)
			if err != nil {
				return 0, fmt.Errorf("failed to read chunk %s: %w", chunkPath, err)
			}

			var dataToRead []byte
			if config.CompressionEnabled() {
				if cr.decoder == nil {
					decoder, err := zstd.NewReader(nil)
					if err != nil {
						return 0, fmt.Errorf("failed to create zstd decoder: %w", err)
					}
					cr.decoder = decoder
				}

				decompressedData, err := cr.decoder.DecodeAll(chunkData, nil)
				if err != nil {
					return 0, fmt.Errorf("failed to decompress chunk: %w", err)
				}
				dataToRead = decompressedData
			} else {
				dataToRead = chunkData
			}

			cr.decompressor = &bytesReader{data: dataToRead, pos: 0}
		}

		n, err = cr.decompressor.Read(p)
		if err == io.EOF {
			// Move to next chunk
			cr.decompressor = nil
			cr.currentIdx++

			if n > 0 {
				return n, nil
			}
			continue
		}

		return n, err
	}
}

// bytesReader implements io.Reader for byte slices
type bytesReader struct {
	data []byte
	pos  int
}

func (br *bytesReader) Read(p []byte) (n int, err error) {
	if br.pos >= len(br.data) {
		return 0, io.EOF
	}
	n = copy(p, br.data[br.pos:])
	br.pos += n
	return n, nil
}

func (cr *ChunkReader) Close() error {
	if cr.decoder != nil {
		cr.decoder.Close()
	}
	if cr.currentFile != nil {
		return cr.currentFile.Close()
	}
	return nil
}
