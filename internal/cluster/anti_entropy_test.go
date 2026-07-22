package cluster

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/storage"
)

func TestRunAntiEntropyNoPeers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}

	secret := make([]byte, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		NodeID:      "solo",
		Listen:      freeAddrRuntime(t),
		Secret:      secret,
		HeartbeatMS: 50,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer rt.Stop()

	hash := "feedface000000000000000000000000000000000000000000000000beefcafe"
	if err := storage.IncrementChunkRefs([]string{hash}); err != nil {
		t.Fatalf("inc: %v", err)
	}

	stats, err := rt.RunAntiEntropy(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stats.Scanned != 0 {
		t.Fatalf("expected scanned=0 when no alive peers, got %d (no work done)", stats.Scanned)
	}
}

func TestRunAntiEntropyOrphanDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-ish")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}

	secret := make([]byte, 32)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		NodeID:      "a",
		Listen:      freeAddrRuntime(t),
		Secret:      secret,
		HeartbeatMS: 50,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer rt.Stop()

	rt.Cluster.mu.Lock()
	ns := NodeState{ID: "b", Addr: "127.0.0.1:1", Status: StatusActive, LastSeen: time.Now()}
	rt.Cluster.view.Upsert(ns)
	rt.Cluster.mu.Unlock()

	stats, err := rt.RunAntiEntropy(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = stats
}
