package cluster

import (
	"testing"
	"time"
)

func TestIsGCLeaderLowestActive(t *testing.T) {
	cases := []struct {
		name  string
		ids   []NodeID
		alive map[NodeID]bool
		self  NodeID
		want  bool
	}{
		{name: "solo self leader", ids: []NodeID{"self"}, alive: map[NodeID]bool{"self": true}, self: "self", want: true},
		{name: "lowest of three", ids: []NodeID{"a", "b", "c"}, alive: map[NodeID]bool{"a": true, "b": true, "c": true}, self: "a", want: true},
		{name: "not lowest", ids: []NodeID{"a", "b", "c"}, alive: map[NodeID]bool{"a": true, "b": true, "c": true}, self: "b", want: false},
		{name: "lowest down promotes next", ids: []NodeID{"a", "b", "c"}, alive: map[NodeID]bool{"a": false, "b": true, "c": true}, self: "b", want: true},
		{name: "all down", ids: []NodeID{"a", "b"}, alive: map[NodeID]bool{"a": false, "b": false}, self: "a", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			peers := map[NodeID]string{}
			for _, id := range tc.ids {
				if id != tc.self {
					peers[id] = "x:1"
				}
			}
			c := New(Config{NodeID: tc.self, Listen: "x:0", Peers: peers, Secret: []byte("k"), HeartbeatMS: 100})

			c.mu.Lock()
			now := time.Now()
			for _, id := range tc.ids {
				ns := c.view.Nodes[id]
				ns.ID = id
				ns.LastSeen = now
				if tc.alive[id] {
					ns.Status = StatusActive
				} else {
					ns.Status = StatusDown
				}
				c.view.Nodes[id] = ns
			}
			c.mu.Unlock()

			got := c.IsGCLeader()
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
