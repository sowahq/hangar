package storage

import (
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

type MetadataStore interface {
	GetRaw(bucket, key string) ([]byte, error)
	PutRaw(bucket, key string, raw []byte) error
	DeleteRaw(bucket, key string) ([]byte, error)
	ListRaw(prefix string, fn func(key, val []byte) bool) error
}

type LocalMetadataStore struct{}

func (LocalMetadataStore) key(bucket, k string) []byte {
	if bucket == "" {
		return []byte("metadata:" + k)
	}
	return []byte("metadata:" + bucket + "/" + k)
}

func (s LocalMetadataStore) GetRaw(bucket, key string) ([]byte, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	return db.Get(s.key(bucket, key))
}

func (s LocalMetadataStore) PutRaw(bucket, key string, raw []byte) error {
	db := database.LocalStore()
	if db == nil {
		return ErrDatabaseClosed
	}
	return db.Put(s.key(bucket, key), raw)
}

func (s LocalMetadataStore) DeleteRaw(bucket, key string) ([]byte, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	k := s.key(bucket, key)
	prev, err := db.Get(k)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, pebble.ErrNotFound
		}
		return nil, err
	}
	if err := db.Delete(k); err != nil {
		return nil, err
	}
	return prev, nil
}

func (s LocalMetadataStore) ListRaw(prefix string, fn func(key, val []byte) bool) error {
	db := database.LocalStore()
	if db == nil {
		return ErrDatabaseClosed
	}
	it, err := db.NewIteratorWithPrefix([]byte(prefix))
	if err != nil {
		return err
	}
	defer it.Close()
	for it.First(); it.Valid(); it.Next() {
		if !fn(it.Key(), it.Value()) {
			return nil
		}
	}
	return nil
}

var (
	metadataMu    sync.RWMutex
	metadataStore MetadataStore = LocalMetadataStore{}
)

func ActiveMetadataStore() MetadataStore {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	return metadataStore
}

func SetMetadataStore(s MetadataStore) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if s == nil {
		metadataStore = LocalMetadataStore{}
		return
	}
	metadataStore = s
}
