package storage

import (
	"errors"
	"io"
	"os"
	"sync"

	"github.com/sowahq/hangar/internal/config"
)

var ErrDatabaseClosed = errors.New("database not initialized")

var ErrChunkNotFound = errors.New("chunk not found")

type ChunkStore interface {
	PutRaw(hash string, payload []byte) error
	OpenRaw(hash string) (io.ReadCloser, error)
	Exists(hash string) (bool, error)
	Delete(hash string) error
}

type LocalChunkStore struct{}

func (LocalChunkStore) PutRaw(hash string, payload []byte) error {
	path := config.ChunkHashToPath(hash)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeChunkRaw(path, payload)
}

func (LocalChunkStore) OpenRaw(hash string) (io.ReadCloser, error) {
	path := config.ChunkHashToPath(hash)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrChunkNotFound
		}
		return nil, err
	}
	return f, nil
}

func (LocalChunkStore) Exists(hash string) (bool, error) {
	path := config.ChunkHashToPath(hash)
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (LocalChunkStore) Delete(hash string) error {
	path := config.ChunkHashToPath(hash)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var (
	chunkMu    sync.RWMutex
	chunkStore ChunkStore = LocalChunkStore{}
)

func ActiveChunkStore() ChunkStore {
	chunkMu.RLock()
	defer chunkMu.RUnlock()
	return chunkStore
}

func SetChunkStore(s ChunkStore) {
	chunkMu.Lock()
	defer chunkMu.Unlock()
	if s == nil {
		chunkStore = LocalChunkStore{}
		return
	}
	chunkStore = s
}
