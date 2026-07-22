package cluster

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sowahq/hangar/internal/database"
)

func setupLayoutDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := database.Init(filepath.Join(dir, "store"), false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
}

func TestLayoutSignAndVerify(t *testing.T) {
	secret := []byte("01234567890123456789012345678901")

	l := &Layout{
		Version: 1,
		Nodes: []LayoutNode{
			{ID: "b", Addr: "10.0.0.2:7000", Zone: "z2", Capacity: 100, Tags: []string{"ssd"}},
			{ID: "a", Addr: "10.0.0.1:7000", Zone: "z1", Capacity: 200, Tags: []string{"hdd"}},
		},
	}
	l.Sign(secret)

	if l.Nodes[0].ID != "a" {
		t.Fatalf("nodes not sorted: %v", l.Nodes)
	}
	if err := l.Verify(secret); err != nil {
		t.Fatalf("verify: %v", err)
	}

	bad := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := l.Verify(bad); !errors.Is(err, ErrLayoutSignature) {
		t.Fatalf("expected sig mismatch, got %v", err)
	}

	l.Nodes[0].Capacity = 999
	if err := l.Verify(secret); !errors.Is(err, ErrLayoutSignature) {
		t.Fatalf("expected sig mismatch after mutation, got %v", err)
	}
}

func TestApplyAndGetLayout(t *testing.T) {
	setupLayoutDB(t)
	secret := []byte("01234567890123456789012345678901")

	l1 := &Layout{Version: 1, Nodes: []LayoutNode{{ID: "a", Addr: "x:1"}}}
	if err := ApplyLayout(l1, secret); err != nil {
		t.Fatalf("apply v1: %v", err)
	}

	v, err := CurrentLayoutVersion()
	if err != nil || v != 1 {
		t.Fatalf("current=%d err=%v", v, err)
	}

	got, err := CurrentLayout()
	if err != nil {
		t.Fatalf("current load: %v", err)
	}
	if err := got.Verify(secret); err != nil {
		t.Fatalf("loaded verify: %v", err)
	}

	l2 := &Layout{Version: 2, Nodes: []LayoutNode{{ID: "a", Addr: "x:1"}, {ID: "b", Addr: "y:2"}}}
	if err := ApplyLayout(l2, secret); err != nil {
		t.Fatalf("apply v2: %v", err)
	}
	if v, _ := CurrentLayoutVersion(); v != 2 {
		t.Fatalf("expected v=2 got %d", v)
	}

	stale := &Layout{Version: 1, Nodes: l1.Nodes}
	if err := ApplyLayout(stale, secret); !errors.Is(err, ErrLayoutStale) {
		t.Fatalf("expected stale, got %v", err)
	}

	v1, err := GetLayout(1)
	if err != nil {
		t.Fatalf("get v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1 mismatch")
	}
}

func TestClusterLoadLayout(t *testing.T) {
	setupLayoutDB(t)
	secret := []byte("01234567890123456789012345678901")

	l := &Layout{Version: 5, Nodes: []LayoutNode{{ID: "a", Addr: "x:1"}}}
	if err := ApplyLayout(l, secret); err != nil {
		t.Fatalf("apply: %v", err)
	}

	c := New(Config{NodeID: "a", Listen: "x:1", Secret: secret, HeartbeatMS: 100})
	if err := c.LoadLayout(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.LayoutVersion() != 5 {
		t.Fatalf("layout v=%d", c.LayoutVersion())
	}
	if c.Layout() == nil || len(c.Layout().Nodes) != 1 {
		t.Fatalf("layout content")
	}
}

func TestClusterApplyLayout(t *testing.T) {
	setupLayoutDB(t)
	secret := []byte("01234567890123456789012345678901")

	c := New(Config{NodeID: "a", Listen: "x:1", Secret: secret, HeartbeatMS: 100})
	l := &Layout{Version: 1, Nodes: []LayoutNode{{ID: "a", Addr: "x:1"}, {ID: "b", Addr: "y:2"}}}
	if err := c.ApplyLayout(l); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if c.LayoutVersion() != 1 {
		t.Fatalf("v=%d", c.LayoutVersion())
	}
}
