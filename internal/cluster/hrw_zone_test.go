package cluster

import (
	"fmt"
	"testing"
)

func TestTopNZoneAwareBackwardCompat(t *testing.T) {
	nodes := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}}

	pure := TopN("k", nodes, 3)
	zone := TopNZoneAware("k", nodes, 3)

	if len(pure) != len(zone) {
		t.Fatalf("len mismatch: pure=%d zone=%d", len(pure), len(zone))
	}
	for i := range pure {
		if pure[i].ID != zone[i].ID {
			t.Fatalf("idx %d: pure=%s zone=%s — empty zones should match pure HRW", i, pure[i].ID, zone[i].ID)
		}
	}
}

func TestTopNZoneAwareDistinctZones(t *testing.T) {
	tests := []struct {
		name    string
		nodes   []Node
		count   int
		wantDiv int
	}{
		{
			name: "2zones_4nodes_count2",
			nodes: []Node{
				{ID: "a1", Zone: "A"},
				{ID: "a2", Zone: "A"},
				{ID: "b1", Zone: "B"},
				{ID: "b2", Zone: "B"},
			},
			count:   2,
			wantDiv: 2,
		},
		{
			name: "3zones_9nodes_count3",
			nodes: []Node{
				{ID: "a1", Zone: "A"}, {ID: "a2", Zone: "A"}, {ID: "a3", Zone: "A"},
				{ID: "b1", Zone: "B"}, {ID: "b2", Zone: "B"}, {ID: "b3", Zone: "B"},
				{ID: "c1", Zone: "C"}, {ID: "c2", Zone: "C"}, {ID: "c3", Zone: "C"},
			},
			count:   3,
			wantDiv: 3,
		},
		{
			name: "3zones_9nodes_count6",
			nodes: []Node{
				{ID: "a1", Zone: "A"}, {ID: "a2", Zone: "A"}, {ID: "a3", Zone: "A"},
				{ID: "b1", Zone: "B"}, {ID: "b2", Zone: "B"}, {ID: "b3", Zone: "B"},
				{ID: "c1", Zone: "C"}, {ID: "c2", Zone: "C"}, {ID: "c3", Zone: "C"},
			},
			count:   6,
			wantDiv: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k := 0; k < 200; k++ {
				key := fmt.Sprintf("obj-%d", k)
				got := TopNZoneAware(key, tc.nodes, tc.count)
				if len(got) != tc.count {
					t.Fatalf("got %d nodes, want %d", len(got), tc.count)
				}

				zones := map[string]int{}
				for _, n := range got[:tc.wantDiv] {
					zones[n.Zone]++
				}
				if len(zones) != tc.wantDiv {
					t.Fatalf("first %d picks span %d zones, want %d (key=%q result=%+v)",
						tc.wantDiv, len(zones), tc.wantDiv, key, got)
				}
			}
		})
	}
}

func TestTopNZoneAwareECPlacementSpread(t *testing.T) {
	nodes := []Node{
		{ID: "a1", Zone: "A"}, {ID: "a2", Zone: "A"},
		{ID: "b1", Zone: "B"}, {ID: "b2", Zone: "B"},
		{ID: "c1", Zone: "C"}, {ID: "c2", Zone: "C"},
	}

	zoneHits := map[string]int{}
	for k := 0; k < 600; k++ {
		key := fmt.Sprintf("chunk:%d", k)
		got := TopNZoneAware(key, nodes, 4)
		first3 := map[string]struct{}{}
		for _, n := range got[:3] {
			first3[n.Zone] = struct{}{}
		}
		if len(first3) != 3 {
			t.Fatalf("first 3 of EC=2+2 placement only span %d zones (key=%q)", len(first3), key)
		}
		for _, n := range got {
			zoneHits[n.Zone]++
		}
	}

	for z, c := range zoneHits {
		if c == 0 {
			t.Fatalf("zone %s never hit", z)
		}
	}
}

func TestTopNZoneAwareStableWithinZone(t *testing.T) {
	nodes := []Node{
		{ID: "a1", Zone: "A"}, {ID: "a2", Zone: "A"},
		{ID: "b1", Zone: "B"}, {ID: "b2", Zone: "B"},
	}

	for k := 0; k < 100; k++ {
		key := fmt.Sprintf("k-%d", k)
		a := TopNZoneAware(key, nodes, 2)
		b := TopNZoneAware(key, nodes, 2)
		if a[0].ID != b[0].ID || a[1].ID != b[1].ID {
			t.Fatalf("non-deterministic: %v vs %v", a, b)
		}
	}
}

func TestTopNZoneAwareCountExceedsNodes(t *testing.T) {
	nodes := []Node{
		{ID: "a1", Zone: "A"},
		{ID: "b1", Zone: "B"},
	}
	got := TopNZoneAware("k", nodes, 5)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
}

func TestTopNZoneAwareZeroCount(t *testing.T) {
	nodes := []Node{{ID: "a", Zone: "A"}}
	if got := TopNZoneAware("k", nodes, 0); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	if got := TopNZoneAware("k", nodes, -1); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestTopNZoneAwareSingleZoneFallsBackToHRW(t *testing.T) {
	nodes := []Node{
		{ID: "n1", Zone: "Z"},
		{ID: "n2", Zone: "Z"},
		{ID: "n3", Zone: "Z"},
		{ID: "n4", Zone: "Z"},
	}
	pure := TopN("k", nodes, 3)
	zone := TopNZoneAware("k", nodes, 3)
	for i := range pure {
		if pure[i].ID != zone[i].ID {
			t.Fatalf("single-zone should match HRW: idx=%d pure=%s zone=%s", i, pure[i].ID, zone[i].ID)
		}
	}
}
