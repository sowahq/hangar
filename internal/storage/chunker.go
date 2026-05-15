package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/blake3"
)

var zstdEncoderPool = sync.Pool{
	New: func() any {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
		if err != nil {
			panic(fmt.Errorf("zstd encoder init: %w", err))
		}
		return enc
	},
}

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

		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			if err := writeChunkAtomic(chunkPath, buf[:n], compressionEnabled); err != nil {
				return nil, "", 0, err
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

func writeChunkAtomic(chunkPath string, data []byte, compress bool) error {
	dir := filepath.Dir(chunkPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".chunk-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	var payload []byte
	if compress {
		encoder := zstdEncoderPool.Get().(*zstd.Encoder)
		payload = encoder.EncodeAll(data, nil)
		zstdEncoderPool.Put(encoder)
	} else {
		payload = data
	}

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, chunkPath); err != nil {
		cleanup()
		return err
	}
	return nil
}

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
