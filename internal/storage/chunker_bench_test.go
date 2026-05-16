package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
)

func setupBenchDB(tb testing.TB) {
	tb.Helper()
	if err := database.Init(tb.TempDir(), true); err != nil {
		tb.Fatalf("database.Init: %v", err)
	}
	tb.Cleanup(func() {
		if db := database.LocalStore(); db != nil {
			_ = db.Close()
		}
	})
}

func setupChunkerBench(tb testing.TB, compress bool) {
	tb.Helper()
	tmp := tb.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	contents := fmt.Sprintf(`data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 4096
enable_compression = %t

[garbage_collection]
interval_hours = 24
`, tmp, compress)
	if err := os.WriteFile(cfgPath, []byte(contents), 0644); err != nil {
		tb.Fatalf("write config: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		tb.Fatalf("LoadServerConfig: %v", err)
	}
	if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
		tb.Fatalf("MkdirAll chunks: %v", err)
	}
}

func BenchmarkChunkAndHashCompressed(b *testing.B) {
	setupBenchDB(b)
	setupChunkerBench(b, true)
	data := deterministicBytes(0x42, 64*1024)

	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if _, _, _, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenChunkCompressed(b *testing.B) {
	setupBenchDB(b)
	setupChunkerBench(b, true)
	data := deterministicBytes(0x55, 64*1024)
	hashes, _, _, err := ChunkAndHash(bytes.NewReader(data), config.ChunksPath())
	if err != nil {
		b.Fatal(err)
	}
	paths := make([]string, len(hashes))
	for i, h := range hashes {
		paths[i] = config.ChunkHashToPath(h)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			rc, err := OpenChunk(p)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := io.Copy(io.Discard, rc); err != nil {
				rc.Close()
				b.Fatal(err)
			}
			rc.Close()
		}
	}
}
