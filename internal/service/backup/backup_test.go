package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sowahq/hangar/internal/database"
)

func writeChunks(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func seedSourceDataDir(t *testing.T, kv map[string][]byte, chunks map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()

	db, err := database.NewPebbleDB(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	for k, v := range kv {
		if err := db.Put([]byte(k), v); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	writeChunks(t, filepath.Join(dir, "chunks"), chunks)
	return dir
}

func TestCreateAndRestoreRoundtrip(t *testing.T) {
	cases := []struct {
		name   string
		kv     map[string][]byte
		chunks map[string][]byte
	}{
		{
			name: "store + chunks",
			kv: map[string][]byte{
				"bucket:alpha":        []byte(`{"name":"alpha"}`),
				"metadata:alpha/key1": []byte(`{"k":"v"}`),
				"chunkref:abcdef":     {0, 0, 0, 0, 0, 0, 0, 1},
			},
			chunks: map[string][]byte{
				"ab/cd/abcdef": bytes.Repeat([]byte{0xAB}, 1024),
				"ef/01/ef0123": []byte("hello"),
			},
		},
		{
			name: "store only",
			kv: map[string][]byte{
				"bucket:beta": []byte(`{"name":"beta"}`),
			},
			chunks: nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := seedSourceDataDir(t, tc.kv, tc.chunks)
			out := filepath.Join(t.TempDir(), "backup")

			m, err := Create(src, out)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if m.Version != ManifestVersion {
				t.Fatalf("manifest version = %d", m.Version)
			}
			if int64(len(tc.chunks)) != m.ChunkFiles {
				t.Fatalf("chunk files = %d want %d", m.ChunkFiles, len(tc.chunks))
			}

			dst := filepath.Join(t.TempDir(), "restored")
			if _, err := Restore(out, dst); err != nil {
				t.Fatalf("Restore: %v", err)
			}

			db, err := database.NewPebbleDB(filepath.Join(dst, "store"))
			if err != nil {
				t.Fatalf("open restored db: %v", err)
			}
			defer db.Close()

			for k, want := range tc.kv {
				got, err := db.Get([]byte(k))
				if err != nil {
					t.Fatalf("get %s: %v", k, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("key %s = %x want %x", k, got, want)
				}
			}

			for rel, want := range tc.chunks {
				got, err := os.ReadFile(filepath.Join(dst, "chunks", rel))
				if err != nil {
					t.Fatalf("read chunk %s: %v", rel, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("chunk %s mismatch", rel)
				}
			}
		})
	}
}

func TestCreateRefusesExistingDestination(t *testing.T) {
	src := seedSourceDataDir(t, map[string][]byte{"k": []byte("v")}, nil)
	out := t.TempDir()

	_, err := Create(src, out)
	if !errors.Is(err, ErrBackupExists) {
		t.Fatalf("Create err = %v want ErrBackupExists", err)
	}
}

func TestRestoreRefusesNonEmptyDataDir(t *testing.T) {
	src := seedSourceDataDir(t, map[string][]byte{"k": []byte("v")}, nil)
	out := filepath.Join(t.TempDir(), "backup")
	if _, err := Create(src, out); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "store"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := Restore(out, dst)
	if !errors.Is(err, ErrRestoreOccupied) {
		t.Fatalf("Restore err = %v want ErrRestoreOccupied", err)
	}
}

func TestRestoreRejectsMissingManifest(t *testing.T) {
	empty := t.TempDir()
	_, err := Restore(empty, t.TempDir())
	if !errors.Is(err, ErrInvalidBackup) {
		t.Fatalf("Restore err = %v want ErrInvalidBackup", err)
	}
}
