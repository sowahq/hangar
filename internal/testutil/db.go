package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/database"
)

func SetupDB(t *testing.T) {
	t.Helper()
	if err := database.Init(t.TempDir(), true); err != nil {
		t.Fatalf("database.Init: %v", err)
	}
	t.Cleanup(func() {
		if db := database.LocalStore(); db != nil {
			_ = db.Close()
		}
	})
}

func SetupServer(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	contents := []byte(`data_directory = "` + tmp + `"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024
enable_compression = false

[garbage_collection]
interval_hours = 24
`)
	if err := os.WriteFile(cfg, contents, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.LoadServerConfig(cfg); err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
		t.Fatalf("MkdirAll chunks: %v", err)
	}
	t.Cleanup(func() {
		if db := database.LocalStore(); db != nil {
			_ = db.Close()
		}
	})
	return tmp
}
