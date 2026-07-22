package cluster

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/database"
	"github.com/sowahq/hangar/internal/storage"
)

func ecTestCluster(t *testing.T, ecData, ecParity int, ids []NodeID) *Cluster {
	t.Helper()
	cfg := Config{
		NodeID:      "self",
		Listen:      "127.0.0.1:1",
		Secret:      []byte("secret"),
		HeartbeatMS: 100,
		ECData:      ecData,
		ECParity:    ecParity,
	}
	c := New(cfg)

	nodes := make([]LayoutNode, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, LayoutNode{ID: id, Addr: string(id) + ":1", Capacity: 1000})
	}
	now := time.Now()

	c.mu.Lock()
	for _, id := range ids {
		c.view.Nodes[id] = NodeState{ID: id, Addr: string(id) + ":1", Status: StatusActive, LastSeen: now}
	}
	c.layout = &Layout{Version: 1, Nodes: nodes}
	c.layoutV = 1
	c.mu.Unlock()
	return c
}

func TestClusteredChunkStoreReplicationFactor(t *testing.T) {
	cases := []struct {
		name             string
		k, m             int
		expectECEnabled  bool
		expectReplicaCnt int
	}{
		{"replicated_default", 0, 0, false, chunkRF},
		{"ec_4_2", 4, 2, true, 6},
		{"ec_6_3", 6, 3, true, 9},
		{"ec_1_1", 1, 1, true, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ids := []NodeID{"self", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
			n := tc.k + tc.m
			if n == 0 {
				n = 2
			}
			c := ecTestCluster(t, tc.k, tc.m, ids[:n+1])
			if c.ECEnabled() != tc.expectECEnabled {
				t.Fatalf("ECEnabled=%v want %v", c.ECEnabled(), tc.expectECEnabled)
			}
			cs := NewClusteredChunkStore(c, nil)
			if got := cs.replicationFactor(); got != tc.expectReplicaCnt {
				t.Fatalf("replicationFactor=%d want %d", got, tc.expectReplicaCnt)
			}
		})
	}
}

func TestClusteredChunkStoreOwnersStableInECMode(t *testing.T) {
	ids := []NodeID{"self", "a", "b", "c", "d", "e"}
	c := ecTestCluster(t, 4, 2, ids)
	cs := NewClusteredChunkStore(c, nil)

	hash := "deadbeef000000000000000000000000000000000000000000000000feedface"

	owners1 := cs.owners(hash)
	if len(owners1) != 6 {
		t.Fatalf("owners len=%d want 6", len(owners1))
	}

	seen := map[NodeID]int{}
	for _, o := range owners1 {
		seen[o]++
	}
	for o, n := range seen {
		if n != 1 {
			t.Fatalf("dup owner %s n=%d", o, n)
		}
	}

	c.mu.Lock()
	for id, ns := range c.view.Nodes {
		if id == "self" {
			continue
		}
		ns.Status = StatusDown
		c.view.Nodes[id] = ns
	}
	c.mu.Unlock()

	owners2 := cs.owners(hash)
	if len(owners2) != len(owners1) {
		t.Fatalf("ec owners len changed by liveness: was %d now %d", len(owners1), len(owners2))
	}
	for i := range owners1 {
		if owners1[i] != owners2[i] {
			t.Fatalf("ec owners[%d] changed under liveness: %s -> %s", i, owners1[i], owners2[i])
		}
	}
}

func TestClusteredChunkStoreOwnersReplicatedFollowsLiveness(t *testing.T) {
	ids := []NodeID{"self", "a", "b"}
	c := ecTestCluster(t, 0, 0, ids)
	cs := NewClusteredChunkStore(c, nil)

	hash := "abc"
	owners := cs.owners(hash)
	if len(owners) != chunkRF {
		t.Fatalf("owners len=%d want %d", len(owners), chunkRF)
	}

	c.mu.Lock()
	ns := c.view.Nodes["a"]
	ns.Status = StatusDown
	c.view.Nodes["a"] = ns
	c.mu.Unlock()

	owners2 := cs.owners(hash)
	for _, o := range owners2 {
		if o == "a" {
			t.Fatalf("replicated mode included down node: %v", owners2)
		}
	}
}

func TestClusteredChunkStorePutECInsufficientOwners(t *testing.T) {
	c := ecTestCluster(t, 4, 2, []NodeID{"self", "a"})
	cs := NewClusteredChunkStore(c, nil)

	err := cs.PutRaw("hash1", []byte("payload"))
	if err == nil {
		t.Fatalf("expected error when EC owners < total")
	}
}

func TestClusteredChunkStoreDeleteCleansShardKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ids := []NodeID{"self", "a", "b", "c", "d", "e"}
	c := ecTestCluster(t, 4, 2, ids)

	pool := NewConnPool(c.Self(), c.Secret(), c.NodeAddr)
	t.Cleanup(pool.Close)
	cs := NewClusteredChunkStore(c, pool)

	hash := "0000000000000000000000000000000000000000000000000000000000abcdef"
	owners := cs.owners(hash)

	selfShard := -1
	for i, o := range owners {
		if o == c.Self() {
			selfShard = i
			break
		}
	}
	if selfShard < 0 {
		t.Fatalf("self never appears in EC owners: %v", owners)
	}

	var local storage.LocalChunkStore
	key := shardKey(hash, selfShard)
	if err := local.PutRaw(key, []byte("shard-data")); err != nil {
		t.Fatalf("seed shard: %v", err)
	}

	if err := cs.Delete(hash); err != nil {
		t.Fatalf("delete: %v", err)
	}

	exists, err := local.Exists(key)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if exists {
		t.Fatalf("self-owned shard %d still present after delete", selfShard)
	}
}

func TestClusteredChunkStoreExistsECThreshold(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	c := ecTestCluster(t, 2, 1, []NodeID{"self", "self2", "self3"})
	c.mu.Lock()
	c.layout = &Layout{
		Version: 2,
		Nodes: []LayoutNode{
			{ID: "self", Addr: "127.0.0.1:1", Capacity: 1000},
			{ID: "self", Addr: "127.0.0.1:1", Capacity: 1000},
			{ID: "self", Addr: "127.0.0.1:1", Capacity: 1000},
		},
	}
	c.mu.Unlock()

	pool := NewConnPool(c.Self(), c.Secret(), c.NodeAddr)
	t.Cleanup(pool.Close)
	cs := NewClusteredChunkStore(c, pool)

	hash := "0000000000000000000000000000000000000000000000000000000000abcdef"

	ok, err := cs.Exists(hash)
	if err != nil {
		t.Fatalf("exists empty: %v", err)
	}
	if ok {
		t.Fatalf("exists=true on empty store")
	}
}
