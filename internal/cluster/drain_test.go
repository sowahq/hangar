package cluster

import (
	"testing"
	"time"
)

func TestRouterSkipsDrainingLayoutNodes(t *testing.T) {
	c := New(Config{NodeID: "self", Listen: "x:0", Secret: []byte("k"), HeartbeatMS: 100})

	now := time.Now()
	c.mu.Lock()
	c.view.Upsert(NodeState{ID: "self", Status: StatusActive, LastSeen: now, Addr: "x:0"})
	c.view.Upsert(NodeState{ID: "a", Status: StatusActive, LastSeen: now, Addr: "a:1"})
	c.view.Upsert(NodeState{ID: "b", Status: StatusActive, LastSeen: now, Addr: "b:2"})
	c.layout = &Layout{
		Version: 1,
		Nodes: []LayoutNode{
			{ID: "self", Addr: "x:0"},
			{ID: "a", Addr: "a:1"},
			{ID: "b", Addr: "b:2", Status: StatusDraining},
		},
	}
	c.layoutV = 1
	c.mu.Unlock()

	for _, n := range c.ObjectShardReplicas("buck", "k", 3) {
		if n == "b" {
			t.Fatalf("draining node b returned by HRW: %v", c.ObjectShardReplicas("buck", "k", 3))
		}
	}

	for _, n := range c.ChunkOwners("deadbeef", 3) {
		if n == "b" {
			t.Fatalf("draining node b owns chunk")
		}
	}
}
