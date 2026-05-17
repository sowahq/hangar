package cluster

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/zeebo/blake3"
)

type DeepScrubStats struct {
	Scanned   int
	Verified  int
	Corrupt   int
	Repaired  int
	Skipped   int
	Errors    int
	StartedAt time.Time
	EndedAt   time.Time
}

func (r *Runtime) RunDeepScrub(ctx context.Context) (stats DeepScrubStats, err error) {
	stats.StartedAt = time.Now()
	defer func() { stats.EndedAt = time.Now() }()

	if r == nil || r.Cluster == nil || !r.Cluster.ECEnabled() {
		return stats, nil
	}

	db := database.LocalStore()
	if db == nil {
		return stats, nil
	}

	enc, err := NewECEncoder(r.Cluster.ECData(), r.Cluster.ECParity())
	if err != nil {
		return stats, err
	}

	seen := map[string]struct{}{}
	hashes := make([]string, 0, 256)

	metaIt, err := db.NewIteratorWithPrefix([]byte("metadata:"))
	if err != nil {
		return stats, err
	}
	for metaIt.First(); metaIt.Valid(); metaIt.Next() {
		var m storage.Metadatas
		if jerr := json.Unmarshal(metaIt.Value(), &m); jerr != nil {
			continue
		}
		for _, h := range m.ChunkHashes {
			if _, ok := seen[h]; ok {
				continue
			}
			seen[h] = struct{}{}
			hashes = append(hashes, h)
		}
	}
	if err := metaIt.Close(); err != nil {
		return stats, err
	}

	refIt, err := db.NewIteratorWithPrefix([]byte("chunkref:"))
	if err != nil {
		return stats, err
	}
	for refIt.First(); refIt.Valid(); refIt.Next() {
		k := refIt.Key()
		if !bytes.HasPrefix(k, []byte("chunkref:")) {
			continue
		}
		h := string(k[len("chunkref:"):])
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		hashes = append(hashes, h)
	}
	if err := refIt.Close(); err != nil {
		return stats, err
	}

	for _, hash := range hashes {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		stats.Scanned++
		r.deepScrubChunk(ctx, hash, enc, &stats)
	}
	return stats, nil
}

func (r *Runtime) deepScrubChunk(ctx context.Context, hash string, enc *ECEncoder, stats *DeepScrubStats) {
	var local storage.LocalChunkStore
	self := r.Cluster.Self()

	owners := r.Cluster.ChunkOwnersStable(hash, enc.Total())
	if len(owners) < enc.Total() {
		stats.Skipped++
		return
	}

	stored := make([][]byte, enc.Total())
	localOwned := make([]bool, enc.Total())
	have := 0
	for i := 0; i < enc.Total(); i++ {
		key := shardKey(hash, i)
		if existsLocal, _ := local.Exists(key); existsLocal {
			rc, err := local.OpenRaw(key)
			if err == nil {
				data, rerr := io.ReadAll(rc)
				_ = rc.Close()
				if rerr == nil {
					stored[i] = data
					localOwned[i] = true
					have++
					continue
				}
			}
		}
		owner := owners[i]
		if owner == self {
			continue
		}
		data, ok := r.pullShardBytes(ctx, owner, key)
		if !ok {
			continue
		}
		stored[i] = data
		have++
	}

	if have < enc.Data() {
		stats.Skipped++
		return
	}

	expectHash, hexErr := hex.DecodeString(hash)
	if hexErr != nil {
		stats.Errors++
		return
	}

	payload, found := findGoodPayload(stored, expectHash, enc)
	if !found {
		stats.Errors++
		return
	}
	canonical, err := enc.Encode(payload)
	if err != nil {
		stats.Errors++
		return
	}

	for i := 0; i < enc.Total(); i++ {
		if stored[i] == nil {
			continue
		}
		stats.Verified++
		if bytes.Equal(stored[i], canonical[i]) {
			continue
		}
		stats.Corrupt++
		if !localOwned[i] {
			continue
		}
		key := shardKey(hash, i)
		if err := local.Delete(key); err != nil {
			stats.Errors++
			continue
		}
		if err := local.PutRaw(key, canonical[i]); err != nil {
			stats.Errors++
			continue
		}
		stats.Repaired++
	}
}

func findGoodPayload(stored [][]byte, expectHash []byte, enc *ECEncoder) ([]byte, bool) {
	if payload, ok := tryDecode(stored, enc); ok {
		if hashMatches(payload, expectHash) {
			return payload, true
		}
	}

	for skip := 0; skip < enc.Total(); skip++ {
		if stored[skip] == nil {
			continue
		}
		work := make([][]byte, enc.Total())
		for i, s := range stored {
			if i == skip || s == nil {
				continue
			}
			work[i] = append([]byte(nil), s...)
		}
		payload, ok := tryDecode(work, enc)
		if !ok {
			continue
		}
		if hashMatches(payload, expectHash) {
			return payload, true
		}
	}
	return nil, false
}

func tryDecode(shards [][]byte, enc *ECEncoder) ([]byte, bool) {
	work := make([][]byte, enc.Total())
	for i, s := range shards {
		if s == nil {
			continue
		}
		work[i] = append([]byte(nil), s...)
	}
	if err := enc.Reconstruct(work); err != nil {
		return nil, false
	}
	payload, err := enc.Decode(work)
	if err != nil {
		return nil, false
	}
	return payload, true
}

func hashMatches(payload, expect []byte) bool {
	sum := blake3.Sum256(payload)
	return bytes.Equal(sum[:], expect)
}
