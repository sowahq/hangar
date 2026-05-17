package cluster

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
)

const (
	walSeqKey       = "mwal:seq"
	walEntryPrefix  = "mwal:e:"
	walCursorPrefix = "mwal:cur:"

	WALRetention   = 24 * time.Hour
	WALCatchupSize = 1000
)

type WALEntry struct {
	Seq    uint64 `json:"seq"`
	Ts     int64  `json:"ts"`
	Op     string `json:"op"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	Value  []byte `json:"value,omitempty"`
}

var walAppendMu sync.Mutex

func walEntryKey(seq uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	return append([]byte(walEntryPrefix), buf[:]...)
}

func walCursorKey(peer NodeID) []byte {
	return []byte(walCursorPrefix + string(peer))
}

func nextWALSeq(db *database.PebbleDB) (uint64, error) {
	data, err := db.Get([]byte(walSeqKey))
	if err != nil && !errors.Is(err, pebble.ErrNotFound) {
		return 0, err
	}
	var seq uint64
	if len(data) == 8 {
		seq = binary.BigEndian.Uint64(data)
	}
	seq++
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	if err := db.PutSilent([]byte(walSeqKey), buf[:]); err != nil {
		return 0, err
	}
	return seq, nil
}

func AppendWAL(op, bucket, key string, value []byte) error {
	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}

	walAppendMu.Lock()
	defer walAppendMu.Unlock()

	seq, err := nextWALSeq(db)
	if err != nil {
		return err
	}
	entry := WALEntry{
		Seq:    seq,
		Ts:     time.Now().UnixMilli(),
		Op:     op,
		Bucket: bucket,
		Key:    key,
		Value:  value,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return db.PutSilent(walEntryKey(seq), data)
}

func GetWALCursor(peer NodeID) (uint64, error) {
	db := database.LocalStore()
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	data, err := db.Get(walCursorKey(peer))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if len(data) != 8 {
		return 0, nil
	}
	return binary.BigEndian.Uint64(data), nil
}

func SetWALCursor(peer NodeID, seq uint64) error {
	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	return db.PutSilent(walCursorKey(peer), buf[:])
}

func ScanWAL(afterSeq uint64, limit int, fn func(*WALEntry) bool) error {
	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}
	it, err := db.NewIteratorWithPrefix([]byte(walEntryPrefix))
	if err != nil {
		return err
	}
	defer it.Close()
	count := 0
	for it.First(); it.Valid(); it.Next() {
		var e WALEntry
		if err := json.Unmarshal(it.Value(), &e); err != nil {
			continue
		}
		if e.Seq <= afterSeq {
			continue
		}
		if !fn(&e) {
			return nil
		}
		count++
		if limit > 0 && count >= limit {
			return nil
		}
	}
	return nil
}

func PurgeOldWAL(maxAge time.Duration) (int, error) {
	db := database.LocalStore()
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	cutoff := time.Now().Add(-maxAge).UnixMilli()
	it, err := db.NewIteratorWithPrefix([]byte(walEntryPrefix))
	if err != nil {
		return 0, err
	}
	var stale [][]byte
	for it.First(); it.Valid(); it.Next() {
		var e WALEntry
		if err := json.Unmarshal(it.Value(), &e); err != nil {
			continue
		}
		if e.Ts < cutoff {
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

type localCatchupHandler struct{}

func (localCatchupHandler) ReplicaCatchup(afterSeq uint64, fn func(*rpc.WALEntry) bool) error {
	return ScanWAL(afterSeq, WALCatchupSize, func(e *WALEntry) bool {
		payload, _ := json.Marshal(e)
		return fn(&rpc.WALEntry{Seq: e.Seq, Ts: e.Ts, OpType: e.Op, Payload: payload})
	})
}

func (r *Runtime) catchUpFromPeer(ctx context.Context, peer NodeID) error {
	cursor, err := GetWALCursor(peer)
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cli, err := r.Pool.Client(cctx, peer)
	if err != nil {
		return err
	}

	stream, err := cli.ReplicaCatchup(cctx, &rpc.CatchupCursor{ShardId: string(peer), LastSeq: cursor})
	if err != nil {
		return err
	}

	applied := 0
	var lastSeq uint64
	for {
		w, err := stream.Recv()
		if err != nil {
			break
		}
		var entry WALEntry
		if err := json.Unmarshal(w.Payload, &entry); err != nil {
			continue
		}
		if entry.Seq <= cursor {
			continue
		}
		if err := applyWALEntry(&entry); err != nil {
			return fmt.Errorf("apply seq %d: %w", entry.Seq, err)
		}
		applied++
		if entry.Seq > lastSeq {
			lastSeq = entry.Seq
		}
	}

	if lastSeq > 0 {
		if err := SetWALCursor(peer, lastSeq); err != nil {
			return err
		}
	}
	return nil
}

func applyWALEntry(e *WALEntry) error {
	var local storage.LocalMetadataStore
	switch e.Op {
	case "put":
		return local.PutRaw(e.Bucket, e.Key, e.Value)
	case "del":
		_, err := local.DeleteRaw(e.Bucket, e.Key)
		if err != nil && !errors.Is(err, pebble.ErrNotFound) {
			return err
		}
		return nil
	}
	return nil
}

func (r *Runtime) StartCatchupLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	prevStatus := map[NodeID]Status{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			view := r.Cluster.View()
			for id, ns := range view.Nodes {
				if id == r.Cluster.Self() {
					continue
				}
				prev := prevStatus[id]
				prevStatus[id] = ns.Status
				if ns.Status != StatusActive {
					continue
				}
				if prev == StatusActive {
					continue
				}
				go func(peer NodeID) {
					_ = r.catchUpFromPeer(ctx, peer)
				}(id)
			}
			_, _ = PurgeOldWAL(WALRetention)
		}
	}
}
