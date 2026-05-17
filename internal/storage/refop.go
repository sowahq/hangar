package storage

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

const refOpPrefix = "refop:"

func refOpKey(id string) []byte {
	return []byte(refOpPrefix + id)
}

func ApplyRefOp(opID string, inc bool, hashes []string) error {
	if opID == "" {
		if inc {
			return IncrementChunkRefs(hashes)
		}
		return DecrementChunkRefs(hashes)
	}

	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}

	if ok, err := db.Exist(refOpKey(opID)); err != nil {
		return err
	} else if ok {
		return nil
	}

	if inc {
		if err := IncrementChunkRefs(hashes); err != nil {
			return err
		}
	} else {
		if err := DecrementChunkRefs(hashes); err != nil {
			return err
		}
	}

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(time.Now().UnixMilli()))
	return db.PutSilent(refOpKey(opID), buf[:])
}

func PurgeOldRefOps(olderThan time.Duration) (int, error) {
	db := database.LocalStore()
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	cutoff := uint64(time.Now().Add(-olderThan).UnixMilli())

	it, err := db.NewIteratorWithPrefix([]byte(refOpPrefix))
	if err != nil {
		return 0, err
	}
	var keys [][]byte
	for it.First(); it.Valid(); it.Next() {
		v := it.Value()
		if len(v) != 8 {
			continue
		}
		ts := binary.BigEndian.Uint64(v)
		if ts < cutoff {
			k := append([]byte(nil), it.Key()...)
			keys = append(keys, k)
		}
	}
	if err := it.Close(); err != nil {
		return 0, err
	}
	for _, k := range keys {
		if err := db.Delete(k); err != nil {
			if !errors.Is(err, pebble.ErrNotFound) {
				return 0, err
			}
		}
	}
	return len(keys), nil
}
