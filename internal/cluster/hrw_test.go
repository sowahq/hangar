package cluster

import (
	"fmt"
	"math"
	"testing"
)

func TestRankNodesDeterministic(t *testing.T) {
	nodes := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}

	tests := []struct {
		name string
		key  string
	}{
		{"short key", "k"},
		{"object hash", "blake3:f9e9d3b97e6d8c4f1a2b3c4d5e6f7081"},
		{"empty key", ""},
		{"unicode", "🚀/路径/键"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := RankNodes(tc.key, nodes)
			for i := 0; i < 5; i++ {
				b := RankNodes(tc.key, nodes)
				if len(a) != len(b) {
					t.Fatalf("length mismatch")
				}
				for i := range a {
					if a[i].ID != b[i].ID {
						t.Fatalf("non-deterministic at index %d: %q vs %q", i, a[i].ID, b[i].ID)
					}
				}
			}
		})
	}
}

func TestRankNodesEmpty(t *testing.T) {
	if got := RankNodes("k", nil); got != nil {
		t.Fatalf("RankNodes(nil) = %v, want nil", got)
	}
	if got := RankNodes("k", []Node{}); len(got) != 0 {
		t.Fatalf("RankNodes(empty) = %v, want []", got)
	}
}

func TestRankNodesPermutationOrderInvariant(t *testing.T) {
	a := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}
	b := []Node{{ID: "n5"}, {ID: "n3"}, {ID: "n1"}, {ID: "n4"}, {ID: "n2"}}

	ra := RankNodes("key-42", a)
	rb := RankNodes("key-42", b)

	for i := range ra {
		if ra[i].ID != rb[i].ID {
			t.Fatalf("input order changed output at %d: %q vs %q", i, ra[i].ID, rb[i].ID)
		}
	}
}

func TestRankNodesUniformDistribution(t *testing.T) {
	nodes := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}
	counts := make(map[string]int)

	const N = 10000
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("obj-%d", i)
		top := RankNodes(key, nodes)[0]
		counts[top.ID]++
	}

	expected := N / len(nodes)
	tolerance := expected / 5

	for id, count := range counts {
		if count < expected-tolerance || count > expected+tolerance {
			t.Errorf("node %s got %d hits, expected %d ±%d", id, count, expected, tolerance)
		}
	}
}

func TestRankNodesStabilityOnAdd(t *testing.T) {
	five := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}
	six := append([]Node{}, five...)
	six = append(six, Node{ID: "n6"})

	const N = 10000
	moved := 0
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("obj-%d", i)
		a := RankNodes(key, five)[0].ID
		b := RankNodes(key, six)[0].ID
		if a != b {
			moved++
		}
	}

	expectedMoved := N / 6
	tolerance := expectedMoved / 2

	if moved < expectedMoved-tolerance || moved > expectedMoved+tolerance {
		t.Errorf("moved %d keys, expected ~%d (±%d)", moved, expectedMoved, tolerance)
	}
}

func TestRankNodesStabilityOnRemove(t *testing.T) {
	five := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}
	four := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}}

	const N = 10000
	moved := 0
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("obj-%d", i)
		a := RankNodes(key, five)[0].ID
		b := RankNodes(key, four)[0].ID
		if a != b {
			moved++
		}
	}

	expectedMoved := N / 5
	tolerance := expectedMoved / 2

	if moved < expectedMoved-tolerance || moved > expectedMoved+tolerance {
		t.Errorf("moved %d keys, expected ~%d (±%d)", moved, expectedMoved, tolerance)
	}
}

func TestRankNodesWeighted(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []Node
		want      map[string]float64
		tolerance float64
	}{
		{
			name: "4:1 ratio",
			nodes: []Node{
				{ID: "big", Weight: 4},
				{ID: "small", Weight: 1},
			},
			want:      map[string]float64{"big": 0.8, "small": 0.2},
			tolerance: 0.05,
		},
		{
			name: "equal weights",
			nodes: []Node{
				{ID: "a", Weight: 2},
				{ID: "b", Weight: 2},
				{ID: "c", Weight: 2},
			},
			want:      map[string]float64{"a": 1.0 / 3, "b": 1.0 / 3, "c": 1.0 / 3},
			tolerance: 0.05,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := make(map[string]int)
			const N = 10000
			for i := 0; i < N; i++ {
				key := fmt.Sprintf("k-%d", i)
				top := RankNodes(key, tc.nodes)[0]
				counts[top.ID]++
			}

			for id, wantFrac := range tc.want {
				gotFrac := float64(counts[id]) / float64(N)
				if math.Abs(gotFrac-wantFrac) > tc.tolerance {
					t.Errorf("node %s: got fraction %.3f, want %.3f ±%.2f", id, gotFrac, wantFrac, tc.tolerance)
				}
			}
		})
	}
}

func TestRankNodesWeightedZeroDefaults(t *testing.T) {
	a := RankNodes("k", []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}})
	b := RankNodes("k", []Node{{ID: "n1", Weight: 1}, {ID: "n2", Weight: 1}, {ID: "n3", Weight: 1}})

	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("zero-weight ≠ weight=1 at %d: %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}

func TestTopN(t *testing.T) {
	nodes := []Node{{ID: "n1"}, {ID: "n2"}, {ID: "n3"}, {ID: "n4"}, {ID: "n5"}}

	tests := []struct {
		n    int
		want int
	}{
		{0, 0},
		{1, 1},
		{3, 3},
		{5, 5},
		{10, 5},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("TopN(%d)", tc.n), func(t *testing.T) {
			got := TopN("k", nodes, tc.n)
			if len(got) != tc.want {
				t.Errorf("TopN(%d) returned %d, want %d", tc.n, len(got), tc.want)
			}
		})
	}

	ranked := RankNodes("k", nodes)
	top3 := TopN("k", nodes, 3)
	for i := 0; i < 3; i++ {
		if top3[i].ID != ranked[i].ID {
			t.Errorf("TopN(3)[%d] = %q, want %q", i, top3[i].ID, ranked[i].ID)
		}
	}
}
