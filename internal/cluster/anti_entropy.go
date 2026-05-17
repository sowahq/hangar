package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
)

type AntiEntropyStats struct {
	Scanned       int
	Pulled        int
	Reconstructed int
	Deleted       int
	Errors        int
	StartedAt     time.Time
	EndedAt       time.Time
}

func (r *Runtime) RunAntiEntropy(ctx context.Context) (stats AntiEntropyStats, err error) {
	stats.StartedAt = time.Now()
	defer func() { stats.EndedAt = time.Now() }()

	if r == nil || r.Cluster == nil {
		return stats, nil
	}

	db := database.LocalStore()
	if db == nil {
		return stats, nil
	}

	view := r.Cluster.View()
	hasAlivePeer := false
	for id, ns := range view.Nodes {
		if id != r.Cluster.Self() && ns.Status == StatusActive {
			hasAlivePeer = true
			break
		}
	}
	if !hasAlivePeer {
		return stats, nil
	}

	seen := map[string]struct{}{}
	hashes := make([]string, 0, 256)

	metaIt, err := db.NewIteratorWithPrefix([]byte("metadata:"))
	if err != nil {
		return stats, err
	}
	for metaIt.First(); metaIt.Valid(); metaIt.Next() {
		var m storage.Metadatas
		if err := json.Unmarshal(metaIt.Value(), &m); err != nil {
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

	if r.Cluster.ECEnabled() {
		enc, err := NewECEncoder(r.Cluster.ECData(), r.Cluster.ECParity())
		if err != nil {
			return stats, err
		}
		for _, hash := range hashes {
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			stats.Scanned++
			r.antiEntropyECChunk(ctx, hash, enc, &stats)
		}
		return stats, nil
	}

	for _, hash := range hashes {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		stats.Scanned++

		owners := r.Cluster.ChunkOwners(hash, chunkRF)
		if len(owners) == 0 {
			continue
		}

		selfOwner := false
		for _, o := range owners {
			if o == r.Cluster.Self() {
				selfOwner = true
				break
			}
		}

		existsLocal, err := (storage.LocalChunkStore{}).Exists(hash)
		if err != nil {
			stats.Errors++
			continue
		}

		switch {
		case selfOwner && !existsLocal:
			if r.pullChunkFromOwners(ctx, hash, owners) {
				stats.Pulled++
			} else {
				stats.Errors++
			}
		case !selfOwner && existsLocal:
			if r.verifyChunkOnOwners(ctx, hash, owners) {
				_ = (storage.LocalChunkStore{}).Delete(hash)
				stats.Deleted++
			}
		}
	}

	return stats, nil
}

func (r *Runtime) antiEntropyECChunk(ctx context.Context, hash string, enc *ECEncoder, stats *AntiEntropyStats) {
	var local storage.LocalChunkStore
	self := r.Cluster.Self()

	owners := r.Cluster.ChunkOwnersStable(hash, enc.Total())
	if len(owners) < enc.Total() {
		stats.Errors++
		return
	}

	myShards := map[int]struct{}{}
	for i, o := range owners {
		if o == self {
			myShards[i] = struct{}{}
		}
	}

	for i := 0; i < enc.Total(); i++ {
		if _, mine := myShards[i]; mine {
			continue
		}
		key := shardKey(hash, i)
		existsLocal, err := local.Exists(key)
		if err != nil {
			stats.Errors++
			continue
		}
		if !existsLocal {
			continue
		}
		owner := owners[i]
		if r.verifyChunkOnOwners(ctx, key, []NodeID{owner}) {
			_ = local.Delete(key)
			stats.Deleted++
			continue
		}
		rc, err := local.OpenRaw(key)
		if err != nil {
			stats.Errors++
			continue
		}
		data, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			stats.Errors++
			continue
		}
		if r.pushShardTo(ctx, owner, key, data) {
			_ = local.Delete(key)
			stats.Deleted++
		} else {
			stats.Errors++
		}
	}

	if len(myShards) == 0 {
		return
	}

	missing := []int{}
	for i := range myShards {
		key := shardKey(hash, i)
		existsLocal, err := local.Exists(key)
		if err != nil {
			stats.Errors++
			continue
		}
		if !existsLocal {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return
	}

	unrepaired := []int{}
	for _, i := range missing {
		key := shardKey(hash, i)
		owner := owners[i]
		if owner == self {
			unrepaired = append(unrepaired, i)
			continue
		}
		if r.pullChunkFromOwners(ctx, key, []NodeID{owner}) {
			stats.Pulled++
		} else {
			unrepaired = append(unrepaired, i)
		}
	}

	if len(unrepaired) == 0 {
		return
	}

	shards := r.collectECShards(ctx, hash, owners, enc, unrepaired)
	if err := enc.Reconstruct(shards); err != nil {
		stats.Errors += len(unrepaired)
		return
	}
	for _, i := range unrepaired {
		if shards[i] == nil {
			stats.Errors++
			continue
		}
		if err := local.PutRaw(shardKey(hash, i), shards[i]); err != nil {
			stats.Errors++
			continue
		}
		stats.Reconstructed++
	}
}

func (r *Runtime) collectECShards(ctx context.Context, hash string, owners []NodeID, enc *ECEncoder, skipLocal []int) [][]byte {
	var local storage.LocalChunkStore
	self := r.Cluster.Self()
	skip := map[int]struct{}{}
	for _, i := range skipLocal {
		skip[i] = struct{}{}
	}

	shards := make([][]byte, enc.Total())
	have := 0
	for i := 0; i < enc.Total() && have < enc.Total(); i++ {
		key := shardKey(hash, i)
		owner := owners[i]
		if owner == self {
			if _, miss := skip[i]; miss {
				continue
			}
			rc, err := local.OpenRaw(key)
			if err != nil {
				continue
			}
			data, rerr := io.ReadAll(rc)
			_ = rc.Close()
			if rerr != nil {
				continue
			}
			shards[i] = data
			have++
			continue
		}
		data, ok := r.pullShardBytes(ctx, owner, key)
		if !ok {
			continue
		}
		shards[i] = data
		have++
	}
	return shards
}

func (r *Runtime) pullShardBytes(ctx context.Context, owner NodeID, key string) ([]byte, bool) {
	cctx, cancel := context.WithTimeout(ctx, chunkRPCTimeout)
	defer cancel()
	cli, err := r.Pool.Client(cctx, owner)
	if err != nil {
		return nil, false
	}
	stream, err := cli.GetChunk(cctx, &rpc.ChunkRef{Hash: key})
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	for {
		msg, rerr := stream.Recv()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, false
		}
		if len(msg.Payload) > 0 {
			buf.Write(msg.Payload)
		}
		if msg.Last {
			break
		}
	}
	if buf.Len() == 0 {
		return nil, false
	}
	return buf.Bytes(), true
}

func (r *Runtime) pushShardTo(ctx context.Context, owner NodeID, key string, payload []byte) bool {
	if owner == r.Cluster.Self() {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, chunkRPCTimeout)
	defer cancel()
	cli, err := r.Pool.Client(cctx, owner)
	if err != nil {
		return false
	}
	stream, err := cli.PutChunk(cctx)
	if err != nil {
		return false
	}
	const frame = 256 * 1024
	for off := 0; off < len(payload); off += frame {
		end := off + frame
		if end > len(payload) {
			end = len(payload)
		}
		last := end == len(payload)
		if err := stream.Send(&rpc.ChunkData{Hash: key, Payload: payload[off:end], Last: last}); err != nil {
			return false
		}
	}
	if len(payload) == 0 {
		if err := stream.Send(&rpc.ChunkData{Hash: key, Last: true}); err != nil {
			return false
		}
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		return false
	}
	return ack.Stored
}

func (r *Runtime) pullChunkFromOwners(ctx context.Context, hash string, owners []NodeID) bool {
	for _, id := range owners {
		if id == r.Cluster.Self() {
			continue
		}
		data, ok := r.pullShardBytes(ctx, id, hash)
		if !ok {
			continue
		}
		var local storage.LocalChunkStore
		if err := local.PutRaw(hash, data); err != nil {
			continue
		}
		return true
	}
	return false
}

func (r *Runtime) verifyChunkOnOwners(ctx context.Context, hash string, owners []NodeID) bool {
	for _, id := range owners {
		if id == r.Cluster.Self() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, metadataRPCWait)
		cli, err := r.Pool.Client(cctx, id)
		if err != nil {
			cancel()
			continue
		}
		pres, err := cli.HasChunk(cctx, &rpc.ChunkRef{Hash: hash})
		cancel()
		if err == nil && pres.Present {
			return true
		}
	}
	return false
}

func (r *Runtime) StartAntiEntropy(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = r.RunAntiEntropy(ctx)
		}
	}
}
