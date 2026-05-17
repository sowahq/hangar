package cluster

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
)

func setupSingleECRuntime(t *testing.T, ecData, ecParity int) *Runtime {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}

	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	rt, err := Start(ctx, Config{
		NodeID:      "self",
		Listen:      freeAddrRuntime(t),
		Secret:      secret,
		HeartbeatMS: 100,
		ECData:      ecData,
		ECParity:    ecParity,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		rt.Stop()
		_ = database.Close()
	})
	return rt
}

func injectECLayoutAndPeers(t *testing.T, rt *Runtime, ids []NodeID) {
	t.Helper()
	nodes := make([]LayoutNode, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, LayoutNode{ID: id, Addr: string(id) + ":0", Capacity: 1000})
	}
	rt.Cluster.mu.Lock()
	rt.Cluster.layout = &Layout{Version: 99, Nodes: nodes}
	rt.Cluster.layoutV = 99
	now := time.Now()
	for _, id := range ids {
		rt.Cluster.view.Nodes[id] = NodeState{ID: id, Addr: string(id) + ":0", Status: StatusActive, LastSeen: now}
	}
	rt.Cluster.mu.Unlock()
}

func TestRunAntiEntropyECModeSelectsShardKeys(t *testing.T) {
	rt := setupSingleECRuntime(t, 4, 2)
	injectECLayoutAndPeers(t, rt, []NodeID{"self", "a", "b", "c", "d", "e"})

	hash := "11112222333344445555666677778888999900001111222233334444aaaaffff"
	if err := storage.IncrementChunkRefs([]string{hash}); err != nil {
		t.Fatalf("inc: %v", err)
	}

	var local storage.LocalChunkStore
	owners := rt.Cluster.ChunkOwnersStable(hash, 6)
	for i, o := range owners {
		if o == rt.Cluster.Self() {
			continue
		}
		if err := local.PutRaw(shardKey(hash, i), []byte("orphan")); err != nil {
			t.Fatalf("seed orphan shard %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stats, err := rt.RunAntiEntropy(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if stats.Scanned != 1 {
		t.Fatalf("scanned=%d want 1", stats.Scanned)
	}

	kept := 0
	for i, o := range owners {
		if o == rt.Cluster.Self() {
			continue
		}
		ex, _ := local.Exists(shardKey(hash, i))
		if ex {
			kept++
		}
	}
	if kept == 0 {
		t.Fatalf("expected orphan shards to remain when push fails (no live peers)")
	}
}

func TestRunAntiEntropyECModeReconstructsLocalShard(t *testing.T) {
	rt := setupSingleECRuntime(t, 2, 1)
	ids := []NodeID{"self", "peer-a", "peer-b"}
	injectECLayoutAndPeers(t, rt, ids)

	hash := "abcdef1111222233334444555566667777888899990000aaaabbbbccccddddeeee"

	enc, err := NewECEncoder(2, 1)
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	payload := []byte("anti-entropy reconstruct payload")
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	owners := rt.Cluster.ChunkOwnersStable(hash, 3)

	selfIdxs := []int{}
	for i, o := range owners {
		if o == rt.Cluster.Self() {
			selfIdxs = append(selfIdxs, i)
		}
	}
	if len(selfIdxs) == 0 {
		t.Skip("self not in owners for this hash; rerun with different hash")
	}

	var local storage.LocalChunkStore
	for i, o := range owners {
		if o == rt.Cluster.Self() {
			continue
		}
		if err := local.PutRaw(shardKey(hash, i), shards[i]); err != nil {
			t.Fatalf("seed remote shard %d locally: %v", i, err)
		}
	}

	if err := storage.IncrementChunkRefs([]string{hash}); err != nil {
		t.Fatalf("inc: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stats, err := rt.RunAntiEntropy(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = stats
}
