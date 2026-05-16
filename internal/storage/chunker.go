package storage

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/pkg/crypto"
	"github.com/klauspost/compress/zstd"
	"github.com/zeebo/blake3"
)

type EncryptParams struct {
	Key         []byte
	NoncePrefix []byte
	PartNumber  uint16
}

func ChunkNonceIdx(part uint16, local uint64) uint64 {
	return (uint64(part) << 40) | local
}

var zstdEncoderPool = sync.Pool{
	New: func() any {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
		if err != nil {
			panic(fmt.Errorf("zstd encoder init: %w", err))
		}
		return enc
	},
}

var chunkBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0)
		return &b
	},
}

func getChunkBuf(size int) *[]byte {
	bp := chunkBufPool.Get().(*[]byte)
	if cap(*bp) < size {
		*bp = make([]byte, size)
	} else {
		*bp = (*bp)[:size]
	}
	return bp
}

func putChunkBuf(bp *[]byte) {
	*bp = (*bp)[:0]
	chunkBufPool.Put(bp)
}

var zstdDecoderPool = sync.Pool{
	New: func() any {
		dec, err := zstd.NewReader(nil)
		if err != nil {
			panic(fmt.Errorf("zstd decoder init: %w", err))
		}
		return dec
	},
}

func GetZstdDecoder() *zstd.Decoder {
	return zstdDecoderPool.Get().(*zstd.Decoder)
}

func PutZstdDecoder(dec *zstd.Decoder) {
	_ = dec.Reset(nil)
	zstdDecoderPool.Put(dec)
}

func ChunkAndHash(r io.Reader, chunkDir string) ([]string, string, int64, error) {
	return ChunkAndHashOpts(r, chunkDir, nil)
}

func ChunkAndHashOpts(r io.Reader, chunkDir string, enc *EncryptParams) (returnedHashes []string, returnedFileHash string, returnedSize int64, returnedErr error) {
	chunkSize := config.ChunkSize()
	compressionEnabled := config.CompressionEnabled()

	var chunkHashes []string
	bufPtr := getChunkBuf(chunkSize)
	defer putChunkBuf(bufPtr)
	buf := *bufPtr
	globalHasher := blake3.New()
	var totalSize int64

	defer func() {
		if returnedErr != nil {
			UnmarkChunksPending(chunkHashes)
		}
	}()

	var localIdx uint64
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

		var payload []byte
		var hashStr string

		if enc != nil {
			plain := buf[:n]
			if compressionEnabled {
				encoder := zstdEncoderPool.Get().(*zstd.Encoder)
				plain = encoder.EncodeAll(plain, nil)
				zstdEncoderPool.Put(encoder)
			}

			nonce, nerr := crypto.ChunkNonce(enc.NoncePrefix, ChunkNonceIdx(enc.PartNumber, localIdx))
			if nerr != nil {
				return nil, "", 0, fmt.Errorf("chunk nonce: %w", nerr)
			}

			sealed, sErr := crypto.Seal(enc.Key, nonce, plain, nil)
			if sErr != nil {
				return nil, "", 0, fmt.Errorf("seal chunk: %w", sErr)
			}

			payload = sealed
			h := blake3.Sum256(sealed)
			hashStr = hex.EncodeToString(h[:])
		} else {
			hash := blake3.Sum256(buf[:n])
			hashStr = hex.EncodeToString(hash[:])
		}

		chunkPath := config.ChunkHashToPath(hashStr)

		MarkChunkPending(hashStr)
		chunkHashes = append(chunkHashes, hashStr)

		if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
			if enc != nil {
				if err := writeChunkRaw(chunkPath, payload); err != nil {
					return nil, "", 0, err
				}
			} else {
				if err := writeChunkAtomic(chunkPath, buf[:n], compressionEnabled); err != nil {
					return nil, "", 0, err
				}
			}
		}

		localIdx++

		if err == io.EOF {
			break
		}
	}

	globalHash := hex.EncodeToString(globalHasher.Sum(nil))
	return chunkHashes, globalHash, totalSize, nil
}

func writeChunkRaw(chunkPath string, data []byte) error {
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

	if _, err := tmp.Write(data); err != nil {
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

func OpenChunkEncrypted(chunkPath string, key, nonce []byte) (io.ReadCloser, error) {
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk file %s: %w", chunkPath, err)
	}

	plain, err := crypto.Open(key, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt chunk %s: %w", chunkPath, err)
	}

	if config.CompressionEnabled() {
		decoder := GetZstdDecoder()
		decoded, dErr := decoder.DecodeAll(plain, nil)
		PutZstdDecoder(decoder)
		if dErr != nil {
			return nil, fmt.Errorf("zstd decode chunk: %w", dErr)
		}
		plain = decoded
	}

	return &memReadCloser{buf: plain}, nil
}

type memReadCloser struct {
	buf []byte
	off int
}

func (m *memReadCloser) Read(p []byte) (int, error) {
	if m.off >= len(m.buf) {
		return 0, io.EOF
	}
	n := copy(p, m.buf[m.off:])
	m.off += n
	return n, nil
}

func (m *memReadCloser) Close() error { return nil }

func OpenChunk(chunkPath string) (io.ReadCloser, error) {
	file, err := os.Open(chunkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open chunk file %s: %w", chunkPath, err)
	}

	if config.CompressionEnabled() {
		decoder := GetZstdDecoder()
		if err := decoder.Reset(file); err != nil {
			PutZstdDecoder(decoder)
			file.Close()
			return nil, fmt.Errorf("failed to reset zstd decoder for %s: %w", chunkPath, err)
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
	PutZstdDecoder(cr.decoder)
	return cr.file.Close()
}
