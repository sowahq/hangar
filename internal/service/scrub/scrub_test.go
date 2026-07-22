package scrub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/internal/testutil"
	"github.com/zeebo/blake3"
)

func hashOf(data []byte) string {
	return fmt.Sprintf("%x", blake3.Sum256(data))
}

func writeChunk(t *testing.T, hash string, data []byte) string {
	t.Helper()
	path := config.ChunkHashToPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestRunVerifiesCleanChunks(t *testing.T) {
	testutil.SetupServer(t)

	payload := []byte("clean chunk payload")
	hash := hashOf(payload)
	writeChunk(t, hash, payload)

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.TotalChunks != 1 {
		t.Errorf("TotalChunks=%d want=1", stats.TotalChunks)
	}
	if stats.Corrupted != 0 {
		t.Errorf("Corrupted=%d want=0", stats.Corrupted)
	}
	if stats.BytesScanned != int64(len(payload)) {
		t.Errorf("BytesScanned=%d want=%d", stats.BytesScanned, len(payload))
	}
}

func TestRunCountsAndSkipsShardFiles(t *testing.T) {
	testutil.SetupServer(t)

	payload := []byte("base chunk payload")
	hash := hashOf(payload)
	basePath := writeChunk(t, hash, payload)

	shardDir := filepath.Dir(basePath)
	for i := 0; i < 3; i++ {
		shardPath := filepath.Join(shardDir, fmt.Sprintf("%s_s%d", hash, i))
		if err := os.WriteFile(shardPath, []byte("ec-shard-bytes"), 0644); err != nil {
			t.Fatalf("write shard %d: %v", i, err)
		}
	}

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.TotalChunks != 1 {
		t.Errorf("TotalChunks=%d want=1 (shards must not count)", stats.TotalChunks)
	}
	if stats.ShardsSkipped != 3 {
		t.Errorf("ShardsSkipped=%d want=3", stats.ShardsSkipped)
	}
	if stats.Corrupted != 0 {
		t.Errorf("Corrupted=%d want=0 (shards should not be content-verified)", stats.Corrupted)
	}
}

func TestRunDetectsAndQuarantinesCorruption(t *testing.T) {
	testutil.SetupServer(t)

	payload := []byte("original")
	hash := hashOf(payload)
	path := writeChunk(t, hash, []byte("tampered bytes that won't match"))

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.Corrupted != 1 {
		t.Errorf("Corrupted=%d want=1", stats.Corrupted)
	}
	if stats.Quarantined != 1 {
		t.Errorf("Quarantined=%d want=1", stats.Quarantined)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("corrupted chunk not moved out of content tree: %v", err)
	}
	qPath := filepath.Join(config.ChunksPath(), ".corrupted", hash)
	if _, err := os.Stat(qPath); err != nil {
		t.Errorf("quarantined file missing at %s: %v", qPath, err)
	}
}

func TestRunDryRunDoesNotMutate(t *testing.T) {
	testutil.SetupServer(t)

	hash := hashOf([]byte("original"))
	path := writeChunk(t, hash, []byte("tampered"))

	stats, err := Run(Opts{DryRun: true})
	if err != nil {
		t.Fatalf("Run dry-run: %v", err)
	}
	if stats.Corrupted != 1 {
		t.Errorf("Corrupted=%d want=1", stats.Corrupted)
	}
	if stats.Quarantined != 0 {
		t.Errorf("Quarantined=%d want=0 (dry-run)", stats.Quarantined)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("dry-run moved corrupted chunk: %v", err)
	}
}

func TestRunDetectsMissingChunkFile(t *testing.T) {
	testutil.SetupServer(t)

	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := storage.IncrementChunkRefs([]string{missing}); err != nil {
		t.Fatalf("IncrementChunkRefs: %v", err)
	}

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.MissingFiles != 1 {
		t.Errorf("MissingFiles=%d want=1", stats.MissingFiles)
	}
}

func TestRunSkipsPendingChunk(t *testing.T) {
	testutil.SetupServer(t)

	hash := hashOf([]byte("pending"))
	writeChunk(t, hash, []byte("garbage that wouldn't verify"))

	storage.MarkChunkPending(hash)
	defer storage.UnmarkChunkPending(hash)

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.TotalChunks != 0 {
		t.Errorf("TotalChunks=%d want=0 (pending should be skipped)", stats.TotalChunks)
	}
	if stats.Corrupted != 0 {
		t.Errorf("Corrupted=%d want=0", stats.Corrupted)
	}
}

func TestRunSkipsQuarantineDir(t *testing.T) {
	testutil.SetupServer(t)

	qDir := filepath.Join(config.ChunksPath(), ".corrupted")
	if err := os.MkdirAll(qDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(qDir, "stale"), []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	payload := []byte("clean")
	hash := hashOf(payload)
	writeChunk(t, hash, payload)

	stats, err := Run(Opts{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.TotalChunks != 1 {
		t.Errorf("TotalChunks=%d want=1 (quarantine dir must be skipped)", stats.TotalChunks)
	}
}

func TestRunRateLimitHonored(t *testing.T) {
	testutil.SetupServer(t)

	payload := make([]byte, 4096)
	hash := hashOf(payload)
	writeChunk(t, hash, payload)

	start := time.Now()
	stats, err := Run(Opts{RateBytesPerSec: 8192})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	if stats.TotalChunks != 1 {
		t.Fatalf("TotalChunks=%d want=1", stats.TotalChunks)
	}
	if elapsed < 400*time.Millisecond {
		t.Errorf("rate limit not honored: elapsed=%v want>=400ms (4KB @ 8KB/s ≈ 500ms)", elapsed)
	}
}

func TestRunContextCancellation(t *testing.T) {
	testutil.SetupServer(t)

	payload := []byte("clean")
	hash := hashOf(payload)
	writeChunk(t, hash, payload)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(Opts{Context: ctx})
	if err == nil {
		t.Errorf("expected cancellation error, got nil")
	}
}
