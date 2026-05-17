package storage

import (
	"testing"
	"time"
)

func TestApplyRefOpIdempotent(t *testing.T) {
	setupStoresTest(t)

	hashes := []string{"aaaa", "bbbb"}
	if err := ApplyRefOp("op-1", true, hashes); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := ApplyRefOp("op-1", true, hashes); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if err := ApplyRefOp("op-1", true, hashes); err != nil {
		t.Fatalf("third apply: %v", err)
	}

	for _, h := range hashes {
		ok, err := IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("ref check %s: %v", h, err)
		}
		if !ok {
			t.Fatalf("hash %s not referenced", h)
		}
	}

	if err := ApplyRefOp("op-2", false, hashes); err != nil {
		t.Fatalf("dec: %v", err)
	}
	if err := ApplyRefOp("op-2", false, hashes); err != nil {
		t.Fatalf("dec repeat: %v", err)
	}

	for _, h := range hashes {
		ok, err := IsChunkReferenced(h)
		if err != nil {
			t.Fatalf("post-dec ref check %s: %v", h, err)
		}
		if ok {
			t.Fatalf("hash %s still referenced after dec", h)
		}
	}
}

func TestApplyRefOpEmptyOpIDFallsThrough(t *testing.T) {
	setupStoresTest(t)
	if err := ApplyRefOp("", true, []string{"xx"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ApplyRefOp("", true, []string{"xx"}); err != nil {
		t.Fatalf("apply repeat: %v", err)
	}
}

func TestPurgeOldRefOps(t *testing.T) {
	setupStoresTest(t)

	if err := ApplyRefOp("old-1", true, []string{"hh"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := ApplyRefOp("new-1", true, []string{"hh"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	purged, err := PurgeOldRefOps(15 * time.Millisecond)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged=%d want 1", purged)
	}
}
