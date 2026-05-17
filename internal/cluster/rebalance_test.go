package cluster

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/config"
)

func TestEagerRebalanceTriggersOnLayoutApply(t *testing.T) {
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

	before := rt.RebalanceCount()

	l := &Layout{
		Version: 1,
		Nodes: []LayoutNode{
			{ID: "solo", Addr: rt.Addr()},
		},
	}
	if err := rt.Cluster.ApplyLayout(l); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if !rt.WaitEagerRebalance(2 * time.Second) {
		t.Fatalf("eager rebalance still running after 2s")
	}

	after := rt.RebalanceCount()
	if after <= before {
		t.Fatalf("expected RebalanceCount to increment after layout apply, before=%d after=%d", before, after)
	}
}

func TestEagerRebalanceDebounced(t *testing.T) {
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

	rt.rebalanceRunning.Store(true)
	defer rt.rebalanceRunning.Store(false)

	before := rt.RebalanceCount()
	rt.triggerEagerRebalance()
	rt.triggerEagerRebalance()
	rt.triggerEagerRebalance()
	time.Sleep(20 * time.Millisecond)
	if got := rt.RebalanceCount(); got != before {
		t.Fatalf("expected count unchanged (in-flight rebalance), before=%d got=%d", before, got)
	}
}

func TestEagerRebalanceDisableToggle(t *testing.T) {
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

	rt.SetEagerRebalanceEnabled(false)
	before := rt.RebalanceCount()
	rt.triggerEagerRebalance()
	time.Sleep(20 * time.Millisecond)
	if got := rt.RebalanceCount(); got != before {
		t.Fatalf("expected disabled rebalance to be skipped, before=%d got=%d", before, got)
	}

	rt.SetEagerRebalanceEnabled(true)
	rt.triggerEagerRebalance()
	if !rt.WaitEagerRebalance(2 * time.Second) {
		t.Fatalf("rebalance never finished")
	}
	if got := rt.RebalanceCount(); got != before+1 {
		t.Fatalf("expected count=%d after re-enable, got=%d", before+1, got)
	}
}
