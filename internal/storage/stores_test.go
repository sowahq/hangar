package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/config"
)

func setupStoresTest(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	contents := fmt.Sprintf(`data_directory = "%s"

[api]
bind_addr = ":0"

[storage]
chunk_size = 1024

[garbage_collection]
interval_hours = 24
`, tmp)
	if err := os.WriteFile(cfg, []byte(contents), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := config.LoadServerConfig(cfg); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
		t.Fatalf("mkdir chunks: %v", err)
	}
}

func TestLocalMetadataStoreRoundtrip(t *testing.T) {
	setupStoresTest(t)

	s := LocalMetadataStore{}
	if err := s.PutRaw("buck", "obj", []byte("hello")); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := s.GetRaw("buck", "obj")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}

	prev, err := s.DeleteRaw("buck", "obj")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if string(prev) != "hello" {
		t.Fatalf("prev=%q", prev)
	}

	if _, err := s.GetRaw("buck", "obj"); err != pebble.ErrNotFound {
		t.Fatalf("post-delete get err=%v", err)
	}
}

func TestLocalMetadataStoreList(t *testing.T) {
	setupStoresTest(t)

	s := LocalMetadataStore{}
	_ = s.PutRaw("b", "a", []byte("1"))
	_ = s.PutRaw("b", "b", []byte("2"))
	_ = s.PutRaw("c", "a", []byte("3"))

	var keys []string
	err := s.ListRaw("metadata:b/", func(k, _ []byte) bool {
		keys = append(keys, string(k))
		return true
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys=%v", keys)
	}
}

func TestActiveStoresDefaultLocal(t *testing.T) {
	if _, ok := ActiveMetadataStore().(LocalMetadataStore); !ok {
		t.Fatalf("default metadata not local")
	}
	if _, ok := ActiveChunkStore().(LocalChunkStore); !ok {
		t.Fatalf("default chunk not local")
	}
	if _, ok := ActiveRefcountStore().(LocalRefcountStore); !ok {
		t.Fatalf("default refcount not local")
	}
}

type fakeMetadataStore struct{ LocalMetadataStore }

func TestSetMetadataStoreSwap(t *testing.T) {
	orig := ActiveMetadataStore()
	defer SetMetadataStore(orig)

	SetMetadataStore(fakeMetadataStore{})
	if _, ok := ActiveMetadataStore().(fakeMetadataStore); !ok {
		t.Fatalf("swap failed")
	}
	SetMetadataStore(nil)
	if _, ok := ActiveMetadataStore().(LocalMetadataStore); !ok {
		t.Fatalf("nil reset failed")
	}
}

func TestLocalChunkStoreRoundtrip(t *testing.T) {
	setupStoresTest(t)

	c := LocalChunkStore{}
	hash := "0000000000000000000000000000000000000000000000000000000000000001"

	if err := c.PutRaw(hash, []byte("payload")); err != nil {
		t.Fatalf("put: %v", err)
	}

	ok, err := c.Exists(hash)
	if err != nil || !ok {
		t.Fatalf("exists=%v err=%v", ok, err)
	}

	rc, err := c.OpenRaw(hash)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	buf := make([]byte, 16)
	n, _ := rc.Read(buf)
	_ = rc.Close()
	if string(buf[:n]) != "payload" {
		t.Fatalf("got %q", buf[:n])
	}

	if err := c.Delete(hash); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ok, _ = c.Exists(hash)
	if ok {
		t.Fatalf("still exists")
	}
}
