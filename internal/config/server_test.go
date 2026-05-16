package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anhostfr/hangar/internal/database"
)

func TestSyncWrites(t *testing.T) {
	tests := []struct {
		name     string
		tomlBody string
		want     bool
	}{
		{
			name: "default is sync",
			tomlBody: `data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024

[garbage_collection]
interval_hours = 24
`,
			want: true,
		},
		{
			name: "explicit true",
			tomlBody: `data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024
sync_writes = true

[garbage_collection]
interval_hours = 24
`,
			want: true,
		},
		{
			name: "explicit false enables NoSync mode",
			tomlBody: `data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024
sync_writes = false

[garbage_collection]
interval_hours = 24
`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			c = nil
			mu.Unlock()
			t.Cleanup(func() { _ = database.Close() })

			tmp := t.TempDir()
			path := filepath.Join(tmp, "config.toml")
			body := []byte(fmt.Sprintf(tc.tomlBody, tmp))
			if err := os.WriteFile(path, body, 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if err := LoadServerConfig(path); err != nil {
				t.Fatalf("LoadServerConfig: %v", err)
			}
			if got := SyncWrites(); got != tc.want {
				t.Errorf("SyncWrites: got=%v want=%v", got, tc.want)
			}
		})
	}
}
