package cluster

import (
	"testing"
	"time"
)

func makeClusterFor(t *testing.T, ids []NodeID, alive map[NodeID]bool) *Cluster {
	t.Helper()
	peers := map[NodeID]string{}
	for _, id := range ids {
		if id != "self" {
			peers[id] = "10.0.0.1:7"
		}
	}
	c := New(Config{NodeID: "self", Listen: "127.0.0.1:1", Peers: peers, Secret: []byte("k"), HeartbeatMS: 100})

	c.mu.Lock()
	now := time.Now()
	for _, id := range ids {
		ns := c.view.Nodes[id]
		ns.ID = id
		ns.LastSeen = now
		if alive[id] {
			ns.Status = StatusActive
		} else {
			ns.Status = StatusDown
		}
		c.view.Nodes[id] = ns
	}
	c.mu.Unlock()
	return c
}

func TestRouterEmptyLayout(t *testing.T) {
	c := makeClusterFor(t, []NodeID{"self"}, map[NodeID]bool{"self": true})

	if got := c.BucketPrimary("b1"); got != "self" {
		t.Fatalf("got %q", got)
	}
}

func TestRouterDeterministic(t *testing.T) {
	c := makeClusterFor(t,
		[]NodeID{"self", "a", "b", "c"},
		map[NodeID]bool{"self": true, "a": true, "b": true, "c": true},
	)

	first := c.ObjectShardPrimary("buck", "key1")
	for i := 0; i < 20; i++ {
		if got := c.ObjectShardPrimary("buck", "key1"); got != first {
			t.Fatalf("non-deterministic %d: %q vs %q", i, got, first)
		}
	}
}

func TestRouterReplicas(t *testing.T) {
	c := makeClusterFor(t,
		[]NodeID{"self", "a", "b", "c"},
		map[NodeID]bool{"self": true, "a": true, "b": true, "c": true},
	)
	got := c.ObjectShardReplicas("b", "k", 3)
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	seen := map[NodeID]int{}
	for _, n := range got {
		seen[n]++
	}
	for n, c := range seen {
		if c != 1 {
			t.Fatalf("dup %s=%d", n, c)
		}
	}
}

func TestRouterChunkOwners(t *testing.T) {
	c := makeClusterFor(t,
		[]NodeID{"self", "a", "b", "c", "d"},
		map[NodeID]bool{"self": true, "a": true, "b": true, "c": true, "d": true},
	)
	got := c.ChunkOwners("deadbeef", 6)
	if len(got) != 5 {
		t.Fatalf("len=%d (asked 6, alive=5)", len(got))
	}
}

func TestRouterSkipsDownNodes(t *testing.T) {
	c := makeClusterFor(t,
		[]NodeID{"self", "a", "b"},
		map[NodeID]bool{"self": true, "a": false, "b": true},
	)
	for _, n := range c.ObjectShardReplicas("buck", "k", 3) {
		if n == "a" {
			t.Fatalf("included down node a: %v", c.ObjectShardReplicas("buck", "k", 3))
		}
	}
}

func TestRouterUsesLayout(t *testing.T) {
	c := makeClusterFor(t,
		[]NodeID{"self", "a", "b"},
		map[NodeID]bool{"self": true, "a": true, "b": true},
	)
	l := &Layout{
		Version: 1,
		Nodes: []LayoutNode{
			{ID: "self", Addr: "x:1", Capacity: 1000},
			{ID: "a", Addr: "y:2", Capacity: 1000},
			{ID: "b", Addr: "z:3", Capacity: 1000},
		},
	}
	c.mu.Lock()
	c.layout = l
	c.layoutV = 1
	c.mu.Unlock()

	if got := c.NodeAddr("a"); got != "y:2" {
		t.Fatalf("addr=%q", got)
	}
}

func TestRouterIsLocal(t *testing.T) {
	c := makeClusterFor(t, []NodeID{"self"}, map[NodeID]bool{"self": true})
	if !c.IsLocal("self") {
		t.Fatalf("self not local")
	}
	if c.IsLocal("other") {
		t.Fatalf("other should not be local")
	}
}
