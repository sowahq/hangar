package storage

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

const pendingPrefix = "pending:"

const PendingLeaseTTL = time.Hour

var pendingMu sync.Mutex

func pendingKey(hash string) []byte {
	return []byte(pendingPrefix + hash)
}

func encodePending(count uint32, ts int64) []byte {
	var buf [12]byte
	binary.BigEndian.PutUint32(buf[0:4], count)
	binary.BigEndian.PutUint64(buf[4:12], uint64(ts))
	return buf[:]
}

func decodePending(data []byte) (uint32, int64, bool) {
	if len(data) != 12 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(data[0:4]), int64(binary.BigEndian.Uint64(data[4:12])), true
}

func MarkChunkPending(hash string) {
	if hash == "" {
		return
	}
	markPendingDelta(hash, 1)
}

func MarkChunksPending(hashes []string) {
	for _, h := range hashes {
		if h == "" {
			continue
		}
		markPendingDelta(h, 1)
	}
}

func UnmarkChunkPending(hash string) {
	if hash == "" {
		return
	}
	markPendingDelta(hash, -1)
}

func UnmarkChunksPending(hashes []string) {
	for _, h := range hashes {
		if h == "" {
			continue
		}
		markPendingDelta(h, -1)
	}
}

func markPendingDelta(hash string, delta int) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	db := database.LocalStore()
	if db == nil {
		return
	}
	key := pendingKey(hash)

	count := uint32(0)
	if data, err := db.Get(key); err == nil {
		if c, _, ok := decodePending(data); ok {
			count = c
		}
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return
	}

	switch {
	case delta > 0:
		count += uint32(delta)
	case delta < 0:
		dec := uint32(-delta)
		if count <= dec {
			_ = db.DeleteSilent(key)
			return
		}
		count -= dec
	default:
		return
	}

	_ = db.PutSilent(key, encodePending(count, time.Now().UnixMilli()))
}

func IsChunkPending(hash string) bool {
	db := database.LocalStore()
	if db == nil {
		return false
	}
	data, err := db.Get(pendingKey(hash))
	if err != nil {
		return false
	}
	count, ts, ok := decodePending(data)
	if !ok {
		return false
	}
	if count == 0 {
		return false
	}
	if time.Since(time.UnixMilli(ts)) > PendingLeaseTTL {
		return false
	}
	return true
}

func PendingChunkCount() int {
	db := database.LocalStore()
	if db == nil {
		return 0
	}
	it, err := db.NewIteratorWithPrefix([]byte(pendingPrefix))
	if err != nil {
		return 0
	}
	defer it.Close()
	n := 0
	for it.First(); it.Valid(); it.Next() {
		n++
	}
	return n
}

func SweepExpiredPending(maxAge time.Duration) (int, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()

	db := database.LocalStore()
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	cutoff := time.Now().Add(-maxAge).UnixMilli()

	it, err := db.NewIteratorWithPrefix([]byte(pendingPrefix))
	if err != nil {
		return 0, err
	}
	var stale [][]byte
	for it.First(); it.Valid(); it.Next() {
		_, ts, ok := decodePending(it.Value())
		if !ok {
			continue
		}
		if ts < cutoff {
			stale = append(stale, append([]byte(nil), it.Key()...))
		}
	}
	if err := it.Close(); err != nil {
		return 0, err
	}
	for _, k := range stale {
		_ = db.DeleteSilent(k)
	}
	return len(stale), nil
}
