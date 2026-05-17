package cluster

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/storage"
)

type localMetadataAdapter struct{}

func (localMetadataAdapter) PutMetadata(bucket, key string, raw []byte) error {
	if err := (storage.LocalMetadataStore{}).PutRaw(bucket, key, raw); err != nil {
		return err
	}
	_ = AppendWAL("put", bucket, key, raw)
	return nil
}

func (localMetadataAdapter) GetMetadata(bucket, key string) ([]byte, bool, error) {
	raw, err := storage.LocalMetadataStore{}.GetRaw(bucket, key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

func (localMetadataAdapter) DeleteMetadata(bucket, key string) ([]byte, bool, error) {
	prev, err := storage.LocalMetadataStore{}.DeleteRaw(bucket, key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	_ = AppendWAL("del", bucket, key, nil)
	return prev, true, nil
}

func (localMetadataAdapter) ListMetadata(prefix string, fn func(key, val []byte) bool) error {
	return storage.LocalMetadataStore{}.ListRaw(prefix, fn)
}

type localChunkAdapter struct{}

func (localChunkAdapter) PutChunk(hash string, payload []byte) error {
	return storage.LocalChunkStore{}.PutRaw(hash, payload)
}

func (localChunkAdapter) OpenChunk(hash string) (io.ReadCloser, error) {
	return storage.LocalChunkStore{}.OpenRaw(hash)
}

func (localChunkAdapter) HasChunk(hash string) (bool, error) {
	return storage.LocalChunkStore{}.Exists(hash)
}

func (localChunkAdapter) DeleteChunkReplica(hash string) error {
	return storage.LocalChunkStore{}.Delete(hash)
}

type localRefcountAdapter struct{}

func (localRefcountAdapter) IncRefs(opID string, hashes []string) error {
	return storage.ApplyRefOp(opID, true, hashes)
}

func (localRefcountAdapter) DecRefs(opID string, hashes []string) error {
	return storage.ApplyRefOp(opID, false, hashes)
}

type localLayoutAdapter struct{}

func (localLayoutAdapter) GetLayout(version uint64) ([]byte, error) {
	l, err := GetLayout(version)
	if err != nil {
		return nil, err
	}
	return json.Marshal(l)
}
