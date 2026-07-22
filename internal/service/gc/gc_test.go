package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/internal/testutil"
)

func writeChunkFile(t *testing.T, hash string, age time.Duration) string {
	t.Helper()
	path := config.ChunkHashToPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mtime := time.Now().Add(-age)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func writeTmpFile(t *testing.T, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(config.ChunksPath(), "ab", "cd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("partial"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	mtime := time.Now().Add(-age)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("Stat(%s): %v", path, err)
	return false
}

func TestRunGarbageCollectionRemovesOrphanPastGrace(t *testing.T) {
	testutil.SetupServer(t)

	const hash = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	path := writeChunkFile(t, hash, 2*time.Hour)

	stats, err := RunGarbageCollection(false)
	if err != nil {
		t.Fatalf("RunGarbageCollection: %v", err)
	}
	if stats.TotalChunks != 1 {
		t.Errorf("TotalChunks: got=%d want=1", stats.TotalChunks)
	}
	if stats.DeletedChunks != 1 {
		t.Errorf("DeletedChunks: got=%d want=1", stats.DeletedChunks)
	}
	if fileExists(t, path) {
		t.Error("orphan chunk past grace not removed")
	}
}

func TestRunGarbageCollectionKeepsReferencedChunk(t *testing.T) {
	testutil.SetupServer(t)

	const hash = "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
	path := writeChunkFile(t, hash, 2*time.Hour)
	if err := storage.IncrementChunkRefs([]string{hash}); err != nil {
		t.Fatalf("IncrementChunkRefs: %v", err)
	}

	stats, err := RunGarbageCollection(false)
	if err != nil {
		t.Fatalf("RunGarbageCollection: %v", err)
	}
	if stats.DeletedChunks != 0 {
		t.Errorf("DeletedChunks: got=%d want=0", stats.DeletedChunks)
	}
	if !fileExists(t, path) {
		t.Error("referenced chunk removed")
	}
}

func TestRunGarbageCollectionKeepsYoungOrphan(t *testing.T) {
	testutil.SetupServer(t)

	const hash = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
	path := writeChunkFile(t, hash, 30*time.Minute)

	stats, err := RunGarbageCollection(false)
	if err != nil {
		t.Fatalf("RunGarbageCollection: %v", err)
	}
	if stats.DeletedChunks != 0 {
		t.Errorf("DeletedChunks: got=%d want=0", stats.DeletedChunks)
	}
	if !fileExists(t, path) {
		t.Error("young orphan within grace removed")
	}
}

func TestRunGarbageCollectionDryRunDeletesNothing(t *testing.T) {
	testutil.SetupServer(t)

	const hash = "0011223344556677889900aabbccddeeff0011223344556677889900aabbccdd"
	path := writeChunkFile(t, hash, 2*time.Hour)

	stats, err := RunGarbageCollection(true)
	if err != nil {
		t.Fatalf("RunGarbageCollection dryRun: %v", err)
	}
	if stats.OrphanChunks != 1 {
		t.Errorf("OrphanChunks: got=%d want=1", stats.OrphanChunks)
	}
	if stats.DeletedChunks != 0 {
		t.Errorf("DeletedChunks dryRun: got=%d want=0", stats.DeletedChunks)
	}
	if !fileExists(t, path) {
		t.Error("dryRun removed orphan")
	}
}

func TestRunGarbageCollectionSkipsPendingChunk(t *testing.T) {
	testutil.SetupServer(t)

	const hash = "deadbeefcafebabe1234567890abcdefdeadbeefcafebabe1234567890abcdef"
	path := writeChunkFile(t, hash, 2*time.Hour)

	storage.MarkChunkPending(hash)
	defer storage.UnmarkChunkPending(hash)

	stats, err := RunGarbageCollection(false)
	if err != nil {
		t.Fatalf("RunGarbageCollection: %v", err)
	}

	if stats.DeletedChunks != 0 {
		t.Errorf("DeletedChunks: got=%d want=0 (pending chunk should be skipped)", stats.DeletedChunks)
	}

	if !fileExists(t, path) {
		t.Error("pending chunk was removed by GC")
	}
}

func TestRunGarbageCollectionReclaimsStaleTmpFile(t *testing.T) {
	testutil.SetupServer(t)

	tmpPath := writeTmpFile(t, ".chunk-stale.tmp", 2*time.Hour)
	youngPath := writeTmpFile(t, ".chunk-fresh.tmp", 5*time.Minute)

	if _, err := RunGarbageCollection(false); err != nil {
		t.Fatalf("RunGarbageCollection: %v", err)
	}
	if fileExists(t, tmpPath) {
		t.Error("stale .tmp past grace not reclaimed")
	}
	if !fileExists(t, youngPath) {
		t.Error("fresh .tmp within grace was reclaimed")
	}
}
