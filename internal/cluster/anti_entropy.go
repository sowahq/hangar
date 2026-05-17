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
	Scanned   int
	Pulled    int
	Deleted   int
	Errors    int
	StartedAt time.Time
	EndedAt   time.Time
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

func (r *Runtime) pullChunkFromOwners(ctx context.Context, hash string, owners []NodeID) bool {
	for _, id := range owners {
		if id == r.Cluster.Self() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, chunkRPCTimeout)
		cli, err := r.Pool.Client(cctx, id)
		if err != nil {
			cancel()
			continue
		}
		stream, err := cli.GetChunk(cctx, &rpc.ChunkRef{Hash: hash})
		if err != nil {
			cancel()
			continue
		}
		var buf bytes.Buffer
		ok := true
		for {
			msg, rerr := stream.Recv()
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				ok = false
				break
			}
			if len(msg.Payload) > 0 {
				buf.Write(msg.Payload)
			}
			if msg.Last {
				break
			}
		}
		cancel()
		if !ok || buf.Len() == 0 {
			continue
		}
		var local storage.LocalChunkStore
		if err := local.PutRaw(hash, buf.Bytes()); err != nil {
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
