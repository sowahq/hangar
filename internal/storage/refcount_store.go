package storage

import "sync"

type RefcountStore interface {
	IncRefs(hashes []string) error
	DecRefs(hashes []string) error
	Referenced(hash string) (bool, error)
}

type LocalRefcountStore struct{}

func (LocalRefcountStore) IncRefs(hashes []string) error { return IncrementChunkRefs(hashes) }

func (LocalRefcountStore) DecRefs(hashes []string) error { return DecrementChunkRefs(hashes) }

func (LocalRefcountStore) Referenced(hash string) (bool, error) { return IsChunkReferenced(hash) }

var (
	refcountMu    sync.RWMutex
	refcountStore RefcountStore = LocalRefcountStore{}
)

func ActiveRefcountStore() RefcountStore {
	refcountMu.RLock()
	defer refcountMu.RUnlock()
	return refcountStore
}

func SetRefcountStore(s RefcountStore) {
	refcountMu.Lock()
	defer refcountMu.Unlock()
	if s == nil {
		refcountStore = LocalRefcountStore{}
		return
	}
	refcountStore = s
}
