package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

const chunkRefPrefix = "chunkref:"

var chunkRefMu sync.Mutex

func chunkRefKey(hash string) []byte {
	return []byte(chunkRefPrefix + hash)
}

func IncrementChunkRefs(hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}

	chunkRefMu.Lock()
	defer chunkRefMu.Unlock()

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	deltas := make(map[string]uint64, len(hashes))
	for _, h := range hashes {
		deltas[h]++
	}

	for h, delta := range deltas {
		key := chunkRefKey(h)
		cur, err := readRefCount(db, key)
		if err != nil {
			return fmt.Errorf("failed to read chunkref %s: %w", h, err)
		}
		if err := writeRefCount(db, key, cur+delta); err != nil {
			return fmt.Errorf("failed to write chunkref %s: %w", h, err)
		}
	}
	return nil
}

func DecrementChunkRefs(hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}

	chunkRefMu.Lock()
	defer chunkRefMu.Unlock()

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	deltas := make(map[string]uint64, len(hashes))
	for _, h := range hashes {
		deltas[h]++
	}

	for h, delta := range deltas {
		key := chunkRefKey(h)
		cur, err := readRefCount(db, key)
		if err != nil {
			return fmt.Errorf("failed to read chunkref %s: %w", h, err)
		}
		if cur <= delta {
			if err := db.Delete(key); err != nil {
				return fmt.Errorf("failed to delete chunkref %s: %w", h, err)
			}
			continue
		}
		if err := writeRefCount(db, key, cur-delta); err != nil {
			return fmt.Errorf("failed to write chunkref %s: %w", h, err)
		}
	}
	return nil
}

func IsChunkReferenced(hash string) (bool, error) {
	db := database.LocalStore()
	if db == nil {
		return false, fmt.Errorf("database not initialized")
	}
	return db.Exist(chunkRefKey(hash))
}

func BootstrapChunkRefs() error {
	chunkRefMu.Lock()
	defer chunkRefMu.Unlock()

	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	refIter, err := db.NewIteratorWithPrefix([]byte(chunkRefPrefix))
	if err != nil {
		return fmt.Errorf("failed to create chunkref iterator: %w", err)
	}
	refIter.First()
	alreadyBootstrapped := refIter.Valid()
	if err := refIter.Close(); err != nil {
		return fmt.Errorf("failed to close chunkref iterator: %w", err)
	}
	if alreadyBootstrapped {
		return nil
	}

	counts := make(map[string]uint64)
	metaIter, err := db.NewIteratorWithPrefix([]byte("metadata:"))
	if err != nil {
		return fmt.Errorf("failed to create metadata iterator: %w", err)
	}
	for metaIter.First(); metaIter.Valid(); metaIter.Next() {
		var m Metadatas
		if err := json.Unmarshal(metaIter.Value(), &m); err != nil {
			continue
		}
		if m.VersionID != "" {
			continue
		}
		for _, h := range m.ChunkHashes {
			counts[h]++
		}
	}
	if err := metaIter.Close(); err != nil {
		return fmt.Errorf("failed to close metadata iterator: %w", err)
	}

	versionIter, err := db.NewIteratorWithPrefix([]byte(versionPrefix))
	if err != nil {
		return fmt.Errorf("failed to create version iterator: %w", err)
	}
	for versionIter.First(); versionIter.Valid(); versionIter.Next() {
		var m Metadatas
		if err := json.Unmarshal(versionIter.Value(), &m); err != nil {
			continue
		}
		for _, h := range m.ChunkHashes {
			counts[h]++
		}
	}
	if err := versionIter.Close(); err != nil {
		return fmt.Errorf("failed to close version iterator: %w", err)
	}

	mpuIter, err := db.NewIteratorWithPrefix([]byte(mpuPartPrefix))
	if err != nil {
		return fmt.Errorf("failed to create mpu part iterator: %w", err)
	}
	for mpuIter.First(); mpuIter.Valid(); mpuIter.Next() {
		var p MultipartPart
		if err := json.Unmarshal(mpuIter.Value(), &p); err != nil {
			continue
		}
		for _, h := range p.ChunkHashes {
			counts[h]++
		}
	}
	if err := mpuIter.Close(); err != nil {
		return fmt.Errorf("failed to close mpu part iterator: %w", err)
	}

	for h, c := range counts {
		if err := writeRefCount(db, chunkRefKey(h), c); err != nil {
			return fmt.Errorf("failed to bootstrap chunkref %s: %w", h, err)
		}
	}
	return nil
}

func readRefCount(db *database.PebbleDB, key []byte) (uint64, error) {
	data, err := db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid chunkref value length: %d", len(data))
	}
	return binary.BigEndian.Uint64(data), nil
}

func writeRefCount(db *database.PebbleDB, key []byte, count uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], count)
	return db.Put(key, buf[:])
}
