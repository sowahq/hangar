package storage

import (
	"sync"
	"testing"

	"github.com/sowahq/hangar/internal/testutil"
)

func TestPendingChunksMarkUnmark(t *testing.T) {
	testutil.SetupDB(t)
	const h = "abc123"

	if IsChunkPending(h) {
		t.Fatalf("expected not pending before Mark")
	}

	MarkChunkPending(h)

	if !IsChunkPending(h) {
		t.Fatalf("expected pending after Mark")
	}

	UnmarkChunkPending(h)

	if IsChunkPending(h) {
		t.Fatalf("expected not pending after Unmark")
	}
}

func TestPendingChunksRefCount(t *testing.T) {
	testutil.SetupDB(t)
	const h = "ref-counted"

	MarkChunkPending(h)
	MarkChunkPending(h)
	MarkChunkPending(h)

	UnmarkChunkPending(h)
	UnmarkChunkPending(h)

	if !IsChunkPending(h) {
		t.Fatalf("expected still pending after 2 Unmarks (refcount=1)")
	}

	UnmarkChunkPending(h)

	if IsChunkPending(h) {
		t.Fatalf("expected not pending after final Unmark")
	}
}

func TestPendingChunksBatchOps(t *testing.T) {
	testutil.SetupDB(t)
	hashes := []string{"a", "b", "c"}

	MarkChunksPending(hashes)

	for _, h := range hashes {
		if !IsChunkPending(h) {
			t.Fatalf("expected %s pending", h)
		}
	}

	UnmarkChunksPending(hashes)

	for _, h := range hashes {
		if IsChunkPending(h) {
			t.Fatalf("expected %s not pending", h)
		}
	}
}

func TestPendingChunksIgnoresEmptyHash(t *testing.T) {
	testutil.SetupDB(t)
	MarkChunkPending("")
	MarkChunksPending([]string{"", "real"})

	if IsChunkPending("") {
		t.Fatalf("empty hash should never be pending")
	}

	if !IsChunkPending("real") {
		t.Fatalf("real hash should be pending")
	}

	UnmarkChunksPending([]string{"", "real"})
}

func TestPendingChunksConcurrentSafe(t *testing.T) {
	testutil.SetupDB(t)
	const h = "race"
	const goroutines = 20
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				MarkChunkPending(h)
				UnmarkChunkPending(h)
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = IsChunkPending(h)
			}
		}()
	}

	wg.Wait()

	if IsChunkPending(h) {
		t.Fatalf("expected refcount zero after balanced ops")
	}
}
