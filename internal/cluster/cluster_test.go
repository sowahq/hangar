package cluster

import (
	"reflect"
	"testing"
	"time"
)

func ns(id string, st Status, gen uint64) NodeState {
	return NodeState{ID: NodeID(id), Addr: id + ":7000", Status: st, Generation: gen, LastSeen: time.Unix(1700000000, 0)}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusUnknown, "unknown"},
		{StatusActive, "active"},
		{StatusSuspect, "suspect"},
		{StatusDown, "down"},
		{StatusDraining, "draining"},
		{Status(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestViewAlive(t *testing.T) {
	tests := []struct {
		name string
		view View
		want []NodeID
	}{
		{
			name: "empty",
			view: NewView(),
			want: []NodeID{},
		},
		{
			name: "all active",
			view: View{Nodes: map[NodeID]NodeState{
				"n2": ns("n2", StatusActive, 1),
				"n1": ns("n1", StatusActive, 1),
				"n3": ns("n3", StatusActive, 1),
			}},
			want: []NodeID{"n1", "n2", "n3"},
		},
		{
			name: "mixed status",
			view: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
				"n2": ns("n2", StatusDown, 1),
				"n3": ns("n3", StatusSuspect, 1),
				"n4": ns("n4", StatusDraining, 1),
				"n5": ns("n5", StatusActive, 1),
			}},
			want: []NodeID{"n1", "n5"},
		},
		{
			name: "none active",
			view: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusDown, 1),
				"n2": ns("n2", StatusSuspect, 1),
			}},
			want: []NodeID{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.view.AliveIDs()
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AliveIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffViews(t *testing.T) {
	tests := []struct {
		name        string
		old, new    View
		wantAdded   []NodeID
		wantRemoved []NodeID
		wantChanged []NodeID
	}{
		{
			name:        "empty to empty",
			old:         NewView(),
			new:         NewView(),
			wantAdded:   nil,
			wantRemoved: nil,
			wantChanged: nil,
		},
		{
			name: "add nodes",
			old:  NewView(),
			new: View{Nodes: map[NodeID]NodeState{
				"n2": ns("n2", StatusActive, 1),
				"n1": ns("n1", StatusActive, 1),
			}},
			wantAdded: []NodeID{"n1", "n2"},
		},
		{
			name: "remove nodes",
			old: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
				"n2": ns("n2", StatusActive, 1),
			}},
			new:         NewView(),
			wantRemoved: []NodeID{"n1", "n2"},
		},
		{
			name: "status change",
			old: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
				"n2": ns("n2", StatusActive, 1),
			}},
			new: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
				"n2": ns("n2", StatusDown, 1),
			}},
			wantChanged: []NodeID{"n2"},
		},
		{
			name: "generation bump",
			old: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
			}},
			new: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 2),
			}},
			wantChanged: []NodeID{"n1"},
		},
		{
			name: "lastseen-only ignored",
			old: View{Nodes: map[NodeID]NodeState{
				"n1": {ID: "n1", Status: StatusActive, Generation: 1, LastSeen: time.Unix(1, 0)},
			}},
			new: View{Nodes: map[NodeID]NodeState{
				"n1": {ID: "n1", Status: StatusActive, Generation: 1, LastSeen: time.Unix(99999, 0)},
			}},
			wantChanged: nil,
		},
		{
			name: "mixed",
			old: View{Nodes: map[NodeID]NodeState{
				"n1": ns("n1", StatusActive, 1),
				"n2": ns("n2", StatusActive, 1),
			}},
			new: View{Nodes: map[NodeID]NodeState{
				"n2": ns("n2", StatusDown, 1),
				"n3": ns("n3", StatusActive, 1),
			}},
			wantAdded:   []NodeID{"n3"},
			wantRemoved: []NodeID{"n1"},
			wantChanged: []NodeID{"n2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DiffViews(tt.old, tt.new)
			if !reflect.DeepEqual(d.Added, tt.wantAdded) {
				t.Errorf("Added = %v, want %v", d.Added, tt.wantAdded)
			}
			if !reflect.DeepEqual(d.Removed, tt.wantRemoved) {
				t.Errorf("Removed = %v, want %v", d.Removed, tt.wantRemoved)
			}
			if !reflect.DeepEqual(d.Changed, tt.wantChanged) {
				t.Errorf("Changed = %v, want %v", d.Changed, tt.wantChanged)
			}
		})
	}
}

func TestViewDiffEmpty(t *testing.T) {
	if !(ViewDiff{}).Empty() {
		t.Fatal("zero ViewDiff should be Empty")
	}
	if (ViewDiff{Added: []NodeID{"n1"}}).Empty() {
		t.Fatal("ViewDiff with adds is not Empty")
	}
}

func TestViewApplyMonotonic(t *testing.T) {
	v := NewView()
	v.Version = 5
	v.Nodes["n1"] = ns("n1", StatusActive, 1)

	tests := []struct {
		name    string
		next    View
		wantOK  bool
		wantVer uint64
	}{
		{"lower version rejected", View{Version: 4, Nodes: map[NodeID]NodeState{"n9": ns("n9", StatusActive, 1)}}, false, 5},
		{"equal version rejected", View{Version: 5, Nodes: map[NodeID]NodeState{"n9": ns("n9", StatusActive, 1)}}, false, 5},
		{"higher accepted", View{Version: 10, Nodes: map[NodeID]NodeState{"n2": ns("n2", StatusActive, 1)}}, true, 10},
		{"higher again", View{Version: 11, Nodes: map[NodeID]NodeState{"n3": ns("n3", StatusActive, 1)}}, true, 11},
		{"old after accept rejected", View{Version: 10, Nodes: map[NodeID]NodeState{}}, false, 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok := v.Apply(tt.next)
			if ok != tt.wantOK {
				t.Errorf("Apply ok=%v, want %v", ok, tt.wantOK)
			}
			if v.Version != tt.wantVer {
				t.Errorf("Version=%d, want %d", v.Version, tt.wantVer)
			}
		})
	}
}

func TestViewApplyClonesNodes(t *testing.T) {
	v := NewView()
	origTags := []string{"a", "b"}
	src := View{Version: 1, Nodes: map[NodeID]NodeState{
		"n1": {ID: "n1", Status: StatusActive, Tags: origTags},
	}}
	if !v.Apply(src) {
		t.Fatal("Apply should accept")
	}

	src.Nodes["n1"] = NodeState{ID: "n1", Status: StatusDown}
	src.Nodes["n2"] = NodeState{ID: "n2", Status: StatusActive}

	got := v.Nodes["n1"]
	if got.Status != StatusActive {
		t.Errorf("internal view mutated by src map write: status=%v", got.Status)
	}
	if _, ok := v.Nodes["n2"]; ok {
		t.Error("internal view mutated by src map add")
	}

	origTags[0] = "zzz"
	if v.Nodes["n1"].Tags[0] == "zzz" {
		t.Error("tags slice not deep-cloned from src")
	}
}

func TestViewUpsertBumpsVersion(t *testing.T) {
	v := NewView()

	if !v.Upsert(ns("n1", StatusActive, 1)) {
		t.Fatal("first insert should bump")
	}
	if v.Version != 1 {
		t.Errorf("version=%d want 1", v.Version)
	}

	if v.Upsert(ns("n1", StatusActive, 1)) {
		t.Fatal("identical upsert should not bump")
	}
	if v.Version != 1 {
		t.Errorf("version=%d want 1", v.Version)
	}

	if !v.Upsert(ns("n1", StatusDown, 1)) {
		t.Fatal("status change should bump")
	}
	if v.Version != 2 {
		t.Errorf("version=%d want 2", v.Version)
	}

	if !v.Upsert(ns("n2", StatusActive, 1)) {
		t.Fatal("new node should bump")
	}
	if v.Version != 3 {
		t.Errorf("version=%d want 3", v.Version)
	}
}

func TestViewRemoveBumpsVersion(t *testing.T) {
	v := NewView()
	v.Upsert(ns("n1", StatusActive, 1))
	v.Upsert(ns("n2", StatusActive, 1))
	verBefore := v.Version

	if !v.Remove("n1") {
		t.Fatal("remove existing should bump")
	}
	if v.Version != verBefore+1 {
		t.Errorf("version=%d want %d", v.Version, verBefore+1)
	}

	if v.Remove("missing") {
		t.Fatal("remove missing should be no-op")
	}
	if v.Version != verBefore+1 {
		t.Errorf("version=%d want %d after no-op", v.Version, verBefore+1)
	}
}
