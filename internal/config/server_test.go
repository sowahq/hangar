package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestClusterConfig(t *testing.T) {
	validSecret := base64.StdEncoding.EncodeToString(make([]byte, 32))
	shortSecret := base64.StdEncoding.EncodeToString(make([]byte, 16))

	base := `data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024

[garbage_collection]
interval_hours = 24
`

	tests := []struct {
		name      string
		extra     string
		wantErr   string
		check     func(t *testing.T)
	}{
		{
			name:  "absent section uses defaults disabled",
			extra: "",
			check: func(t *testing.T) {
				if ClusterEnabled() {
					t.Errorf("ClusterEnabled: want false")
				}
				if got := ECDataShards(); got != 4 {
					t.Errorf("ECDataShards: got=%d want=4", got)
				}
				if got := ECParityShards(); got != 2 {
					t.Errorf("ECParityShards: got=%d want=2", got)
				}
				if got := MetaShards(); got != 256 {
					t.Errorf("MetaShards: got=%d want=256", got)
				}
				if got := HeartbeatMS(); got != 500 {
					t.Errorf("HeartbeatMS: got=%d want=500", got)
				}
				if MetadataSyncQuorum() {
					t.Errorf("MetadataSyncQuorum: want false")
				}
				if ClusterSharedSecret() != nil {
					t.Errorf("ClusterSharedSecret: want nil when disabled")
				}
			},
		},
		{
			name: "enabled full config",
			extra: fmt.Sprintf(`
[cluster]
enabled = true
node_id = "n1"
listen = ":7000"
shared_secret_b64 = %q
seeds = ["10.0.0.2:7000", "10.0.0.3:7000"]
ec_data_shards = 6
ec_parity_shards = 3
meta_shards = 512
heartbeat_ms = 250
metadata_sync_quorum = true
`, validSecret),
			check: func(t *testing.T) {
				if !ClusterEnabled() {
					t.Fatal("ClusterEnabled: want true")
				}
				if got := ClusterNodeID(); got != "n1" {
					t.Errorf("ClusterNodeID: got=%q", got)
				}
				if got := ClusterListen(); got != ":7000" {
					t.Errorf("ClusterListen: got=%q", got)
				}
				seeds := ClusterSeeds()
				if len(seeds) != 2 || seeds[0] != "10.0.0.2:7000" || seeds[1] != "10.0.0.3:7000" {
					t.Errorf("ClusterSeeds: got=%v", seeds)
				}
				secret := ClusterSharedSecret()
				if len(secret) != 32 {
					t.Errorf("ClusterSharedSecret: len=%d want=32", len(secret))
				}
				if got := ECDataShards(); got != 6 {
					t.Errorf("ECDataShards: got=%d want=6", got)
				}
				if got := ECParityShards(); got != 3 {
					t.Errorf("ECParityShards: got=%d want=3", got)
				}
				if got := MetaShards(); got != 512 {
					t.Errorf("MetaShards: got=%d want=512", got)
				}
				if got := HeartbeatMS(); got != 250 {
					t.Errorf("HeartbeatMS: got=%d want=250", got)
				}
				if !MetadataSyncQuorum() {
					t.Errorf("MetadataSyncQuorum: want true")
				}
			},
		},
		{
			name: "enabled missing node_id defaults to hostname",
			extra: fmt.Sprintf(`
[cluster]
enabled = true
listen = ":7000"
shared_secret_b64 = %q
`, validSecret),
			check: func(t *testing.T) {
				if ClusterNodeID() == "" {
					t.Fatal("expected hostname default for node_id")
				}
			},
		},
		{
			name: "enabled missing listen errors",
			extra: fmt.Sprintf(`
[cluster]
enabled = true
node_id = "n1"
shared_secret_b64 = %q
`, validSecret),
			wantErr: "listen required",
		},
		{
			name: `enabled missing shared_secret errors`,
			extra: `
[cluster]
enabled = true
node_id = "n1"
listen = ":7000"
`,
			wantErr: "shared_secret_b64 required",
		},
		{
			name: "enabled bad b64 errors",
			extra: `
[cluster]
enabled = true
node_id = "n1"
listen = ":7000"
shared_secret_b64 = "@@@not-base64@@@"
`,
			wantErr: "not valid base64",
		},
		{
			name: "enabled wrong key length errors",
			extra: fmt.Sprintf(`
[cluster]
enabled = true
node_id = "n1"
listen = ":7000"
shared_secret_b64 = %q
`, shortSecret),
			wantErr: "must decode to 32 bytes",
		},
		{
			name: "disabled section ignores other fields",
			extra: `
[cluster]
enabled = false
`,
			check: func(t *testing.T) {
				if ClusterEnabled() {
					t.Errorf("ClusterEnabled: want false")
				}
				if ClusterSharedSecret() != nil {
					t.Errorf("ClusterSharedSecret: want nil")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			c = nil
			clusterSharedSecret = nil
			mu.Unlock()
			t.Cleanup(func() { _ = database.Close() })

			tmp := t.TempDir()
			path := filepath.Join(tmp, "config.toml")
			body := []byte(fmt.Sprintf(base, tmp) + tc.extra)
			if err := os.WriteFile(path, body, 0644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			err := LoadServerConfig(path)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadServerConfig: want error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("LoadServerConfig: err=%q want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadServerConfig: %v", err)
			}
			if tc.check != nil {
				tc.check(t)
			}
		})
	}
}
