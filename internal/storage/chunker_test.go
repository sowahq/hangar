package storage

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/testutil"
	"github.com/zeebo/blake3"
)

func setupChunker(t *testing.T, compress bool) {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	contents := fmt.Sprintf(`data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024
enable_compression = %t

[garbage_collection]
interval_hours = 24
`, tmp, compress)
	if err := os.WriteFile(cfg, []byte(contents), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.LoadServerConfig(cfg); err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
		t.Fatalf("MkdirAll chunks: %v", err)
	}
}

func deterministicBytes(seed byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = seed + byte(i%251)
	}
	return out
}

func TestChunkAndHash(t *testing.T) {
	tests := []struct {
		name       string
		size       int
		wantChunks int
	}{
		{"empty input", 0, 0},
		{"single byte", 1, 1},
		{"under chunk", 512, 1},
		{"exact chunk", 1024, 1},
		{"chunk plus one", 1025, 2},
		{"three full chunks", 3072, 3},
		{"three and a half chunks", 1024*3 + 200, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			setupChunker(t, false)
			data := deterministicBytes(0x42, tc.size)

			hashes, globalHash, total, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath())
			if err != nil {
				t.Fatalf("ChunkAndHash: %v", err)
			}
			if len(hashes) != tc.wantChunks {
				t.Errorf("chunks=%d want=%d", len(hashes), tc.wantChunks)
			}
			if total != int64(tc.size) {
				t.Errorf("totalSize=%d want=%d", total, tc.size)
			}
			want := fmt.Sprintf("%x", blake3.Sum256(data))
			if globalHash != want {
				t.Errorf("globalHash=%s want=%s", globalHash, want)
			}
			for _, h := range hashes {
				if _, err := os.Stat(config.ChunkHashToPath(h)); err != nil {
					t.Errorf("chunk file missing for %s: %v", h, err)
				}
			}
		})
	}
}

func TestChunkAndHashDedup(t *testing.T) {
	testutil.SetupDB(t)
	setupChunker(t, false)

	data := deterministicBytes(0x11, 3072)
	first, _, _, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath())
	if err != nil {
		t.Fatalf("first ChunkAndHash: %v", err)
	}
	statsBefore := chunkFileCount(t)

	second, _, _, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath())
	if err != nil {
		t.Fatalf("second ChunkAndHash: %v", err)
	}
	statsAfter := chunkFileCount(t)

	if statsBefore != statsAfter {
		t.Errorf("chunk file count changed: before=%d after=%d (dedup broken)", statsBefore, statsAfter)
	}
	if len(first) != len(second) {
		t.Fatalf("hash slice lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("hash %d differs across runs: %s vs %s", i, first[i], second[i])
		}
	}
}

func TestChunkAndHashDistinctContent(t *testing.T) {
	testutil.SetupDB(t)
	setupChunker(t, false)

	a := deterministicBytes(0xAA, 1024)
	b := deterministicBytes(0xBB, 1024)
	hashesA, _, _, err := ChunkAndHash(bytes.NewReader(a), config.ChunksPath())
	if err != nil {
		t.Fatalf("ChunkAndHash A: %v", err)
	}
	hashesB, _, _, err := ChunkAndHash(bytes.NewReader(b), config.ChunksPath())
	if err != nil {
		t.Fatalf("ChunkAndHash B: %v", err)
	}
	if hashesA[0] == hashesB[0] {
		t.Errorf("distinct content produced identical chunk hash: %s", hashesA[0])
	}
	if chunkFileCount(t) != 2 {
		t.Errorf("expected 2 chunk files, got %d", chunkFileCount(t))
	}
}

func TestRoundtrip(t *testing.T) {
	tests := []struct {
		name       string
		compress   bool
		size       int
	}{
		{"uncompressed small", false, 256},
		{"uncompressed multi-chunk", false, 1024*2 + 128},
		{"compressed small", true, 256},
		{"compressed multi-chunk", true, 1024*2 + 128},
		{"compressed random (low ratio)", true, 4096},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupDB(t)
			setupChunker(t, tc.compress)

			var data []byte
			if tc.name == "compressed random (low ratio)" {
				data = make([]byte, tc.size)
				if _, err := rand.Read(data); err != nil {
					t.Fatalf("rand.Read: %v", err)
				}
			} else {
				data = deterministicBytes(0x77, tc.size)
			}

			hashes, _, total, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath())
			if err != nil {
				t.Fatalf("ChunkAndHash: %v", err)
			}
			if total != int64(tc.size) {
				t.Fatalf("totalSize=%d want=%d", total, tc.size)
			}

			var got bytes.Buffer
			for _, h := range hashes {
				rc, err := OpenChunk(config.ChunkHashToPath(h))
				if err != nil {
					t.Fatalf("OpenChunk(%s): %v", h, err)
				}
				if _, err := io.Copy(&got, rc); err != nil {
					rc.Close()
					t.Fatalf("io.Copy: %v", err)
				}
				rc.Close()
			}
			if !bytes.Equal(got.Bytes(), data) {
				t.Errorf("roundtrip mismatch: got %d bytes, want %d bytes", got.Len(), len(data))
			}
		})
	}
}

func TestWriteChunkAtomicNoTmpLeak(t *testing.T) {
	testutil.SetupDB(t)
	setupChunker(t, false)

	data := deterministicBytes(0x33, 512)
	if _, _, _, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath()); err != nil {
		t.Fatalf("ChunkAndHash: %v", err)
	}

	count := 0
	err := filepath.Walk(config.ChunksPath(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".tmp" {
			t.Errorf("stray tmp file: %s", path)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 chunk file, got %d", count)
	}
}

func chunkFileCount(t *testing.T) int {
	t.Helper()
	count := 0
	err := filepath.Walk(config.ChunksPath(), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) != ".tmp" {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return count
}
