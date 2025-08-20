package storage

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/blake3"
)

func ChunkAndHash(r io.Reader, chunkDir string) ([]string, string, int64, error) {
	chunkSize := config.ChunkSize()
	compressionEnabled := config.CompressionEnabled()

	var chunkHashes []string
	buf := make([]byte, chunkSize)
	globalHasher := blake3.New()
	var totalSize int64

	for {
		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, "", 0, err
		}
		if n == 0 {
			break
		}

		totalSize += int64(n)
		globalHasher.Write(buf[:n])

		hash := blake3.Sum256(buf[:n])
		hashStr := fmt.Sprintf("%x", hash[:])
		chunkPath := config.ChunkHashToPath(hashStr)
		chunksPathPart := strings.Split(chunkPath, "/")

		// Only write chunk if it doesn't exist (deduplication)
		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			if err := os.MkdirAll(strings.Join(chunksPathPart[:len(chunksPathPart)-1], "/"), 0755); err != nil {
				return nil, "", 0, err
			}

			f, err := os.Create(chunkPath)
			if err != nil {
				return nil, "", 0, err
			}

			var dataToWrite []byte
			if compressionEnabled {
				encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
				if err != nil {
					f.Close()
					return nil, "", 0, fmt.Errorf("failed to create zstd encoder: %w", err)
				}
				dataToWrite = encoder.EncodeAll(buf[:n], nil)
				encoder.Close()
			} else {
				dataToWrite = buf[:n]
			}

			_, writeErr := f.Write(dataToWrite)
			f.Close()
			if writeErr != nil {
				return nil, "", 0, writeErr
			}
		}

		chunkHashes = append(chunkHashes, hashStr)
		if err == io.EOF {
			break
		}
	}

	globalHash := fmt.Sprintf("%x", globalHasher.Sum(nil))
	return chunkHashes, globalHash, totalSize, nil
}

// OpenChunk opens a chunk file for reading, handling decompression if needed
func OpenChunk(chunkPath string) (io.ReadCloser, error) {
	file, err := os.Open(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open chunk file %s: %w", chunkPath, err)
	}

	if config.CompressionEnabled() {
		decoder, err := zstd.NewReader(file)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to create zstd decoder for %s: %w", chunkPath, err)
		}

		return &chunkReader{
			decoder: decoder,
			file:    file,
		}, nil
	}

	return file, nil
}

// chunkReader wraps a zstd decoder and underlying file for proper cleanup
type chunkReader struct {
	decoder *zstd.Decoder
	file    *os.File
}

func (cr *chunkReader) Read(p []byte) (int, error) {
	return cr.decoder.Read(p)
}

func (cr *chunkReader) Close() error {
	cr.decoder.Close()
	return cr.file.Close()
}
