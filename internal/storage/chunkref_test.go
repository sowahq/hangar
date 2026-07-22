package storage

import (
	"testing"

	"github.com/sowahq/hangar/internal/database"
	"github.com/sowahq/hangar/internal/testutil"
)

func currentCount(t *testing.T, hash string) uint64 {
	t.Helper()
	db := database.LocalStore()
	n, err := readRefCount(db, chunkRefKey(hash))
	if err != nil {
		t.Fatalf("readRefCount(%s): %v", hash, err)
	}
	return n
}

func TestIncrementChunkRefs(t *testing.T) {
	tests := []struct {
		name   string
		ops    [][]string
		expect map[string]uint64
	}{
		{
			name:   "empty input is no-op",
			ops:    [][]string{{}},
			expect: map[string]uint64{},
		},
		{
			name:   "single hash single increment",
			ops:    [][]string{{"aaa"}},
			expect: map[string]uint64{"aaa": 1},
		},
		{
			name:   "duplicate hashes in one call accumulate",
			ops:    [][]string{{"aaa", "aaa", "bbb"}},
			expect: map[string]uint64{"aaa": 2, "bbb": 1},
		},
		{
			name:   "successive calls add up",
			ops:    [][]string{{"aaa"}, {"aaa", "bbb"}, {"bbb"}},
			expect: map[string]uint64{"aaa": 2, "bbb": 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			for _, op := range tc.ops {
				if err := IncrementChunkRefs(op); err != nil {
					t.Fatalf("IncrementChunkRefs(%v): %v", op, err)
				}
			}
			for h, want := range tc.expect {
				if got := currentCount(t, h); got != want {
					t.Errorf("hash %s: count=%d want=%d", h, got, want)
				}
			}
		})
	}
}

func TestDecrementChunkRefs(t *testing.T) {
	tests := []struct {
		name        string
		seed        []string
		dec         []string
		expectCount map[string]uint64
		expectGone  []string
	}{
		{
			name:       "decrement to zero removes key",
			seed:       []string{"aaa"},
			dec:        []string{"aaa"},
			expectGone: []string{"aaa"},
		},
		{
			name:        "decrement leaves remainder",
			seed:        []string{"aaa", "aaa", "aaa"},
			dec:         []string{"aaa"},
			expectCount: map[string]uint64{"aaa": 2},
		},
		{
			name:        "decrement duplicates in one call",
			seed:        []string{"aaa", "aaa", "aaa"},
			dec:         []string{"aaa", "aaa"},
			expectCount: map[string]uint64{"aaa": 1},
		},
		{
			name:       "decrement past zero clamps to delete",
			seed:       []string{"aaa"},
			dec:        []string{"aaa", "aaa", "aaa"},
			expectGone: []string{"aaa"},
		},
		{
			name:       "empty decrement is no-op",
			seed:       []string{"aaa"},
			dec:        []string{},
			expectCount: map[string]uint64{"aaa": 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			if err := IncrementChunkRefs(tc.seed); err != nil {
				t.Fatalf("seed Increment: %v", err)
			}
			if err := DecrementChunkRefs(tc.dec); err != nil {
				t.Fatalf("Decrement: %v", err)
			}
			for h, want := range tc.expectCount {
				if got := currentCount(t, h); got != want {
					t.Errorf("hash %s: count=%d want=%d", h, got, want)
				}
			}
			for _, h := range tc.expectGone {
				ok, err := IsChunkReferenced(h)
				if err != nil {
					t.Fatalf("IsChunkReferenced(%s): %v", h, err)
				}
				if ok {
					t.Errorf("hash %s: still referenced, want gone", h)
				}
			}
		})
	}
}

func TestIsChunkReferenced(t *testing.T) {
	testutil.SetupDB(t)
	if err := IncrementChunkRefs([]string{"present"}); err != nil {
		t.Fatalf("Increment: %v", err)
	}

	tests := []struct {
		name string
		hash string
		want bool
	}{
		{"existing", "present", true},
		{"missing", "absent", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsChunkReferenced(tc.hash)
			if err != nil {
				t.Fatalf("IsChunkReferenced: %v", err)
			}
			if got != tc.want {
				t.Errorf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestBootstrapChunkRefs(t *testing.T) {
	tests := []struct {
		name     string
		seedMeta []*Metadatas
		preRefs  []string
		expect   map[string]uint64
	}{
		{
			name:     "no metadata yields no entries",
			seedMeta: nil,
			expect:   map[string]uint64{},
		},
		{
			name: "metadata rebuilds counts including dedup",
			seedMeta: []*Metadatas{
				{Key: "obj-a", ChunkHashes: []string{"aaa", "bbb"}},
				{Key: "obj-b", ChunkHashes: []string{"bbb", "ccc"}},
				{Key: "obj-c", ChunkHashes: []string{"ccc", "ccc"}},
			},
			expect: map[string]uint64{"aaa": 1, "bbb": 2, "ccc": 3},
		},
		{
			name: "idempotent: pre-existing chunkref short-circuits bootstrap",
			seedMeta: []*Metadatas{
				{Key: "obj-a", ChunkHashes: []string{"aaa", "bbb"}},
			},
			preRefs: []string{"zzz"},
			expect:  map[string]uint64{"zzz": 1, "aaa": 0, "bbb": 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			for _, m := range tc.seedMeta {
				if err := StoreMetadataInBucket("b", m); err != nil {
					t.Fatalf("StoreMetadataInBucket: %v", err)
				}
			}
			if len(tc.preRefs) > 0 {
				if err := IncrementChunkRefs(tc.preRefs); err != nil {
					t.Fatalf("seed Increment: %v", err)
				}
			}
			if err := BootstrapChunkRefs(); err != nil {
				t.Fatalf("BootstrapChunkRefs: %v", err)
			}
			for h, want := range tc.expect {
				if got := currentCount(t, h); got != want {
					t.Errorf("hash %s: count=%d want=%d", h, got, want)
				}
			}
		})
	}
}

func TestBootstrapChunkRefsIdempotentWhenAlreadyBuilt(t *testing.T) {
	testutil.SetupDB(t)
	if err := StoreMetadataInBucket("b", &Metadatas{Key: "obj", ChunkHashes: []string{"aaa"}}); err != nil {
		t.Fatalf("StoreMetadataInBucket: %v", err)
	}
	if err := BootstrapChunkRefs(); err != nil {
		t.Fatalf("first BootstrapChunkRefs: %v", err)
	}
	if got := currentCount(t, "aaa"); got != 1 {
		t.Fatalf("after bootstrap: count=%d want=1", got)
	}
	if err := IncrementChunkRefs([]string{"aaa"}); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if got := currentCount(t, "aaa"); got != 2 {
		t.Fatalf("after manual inc: count=%d want=2", got)
	}
	if err := BootstrapChunkRefs(); err != nil {
		t.Fatalf("second BootstrapChunkRefs: %v", err)
	}
	if got := currentCount(t, "aaa"); got != 2 {
		t.Errorf("second bootstrap mutated count: got=%d want=2 (must short-circuit)", got)
	}
}
