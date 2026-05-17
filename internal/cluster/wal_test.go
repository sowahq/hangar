package cluster

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/database"
)

func setupWALDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := database.Init(filepath.Join(dir, "store"), false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
}

func TestAppendAndScanWAL(t *testing.T) {
	setupWALDB(t)

	for i := 0; i < 5; i++ {
		if err := AppendWAL("put", "b", "k", []byte("v")); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	var got []uint64
	err := ScanWAL(0, 0, func(e *WALEntry) bool {
		got = append(got, e.Seq)
		return true
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d entries", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("seq not monotonic: %v", got)
		}
	}
}

func TestScanWALAfterSeq(t *testing.T) {
	setupWALDB(t)

	for i := 0; i < 5; i++ {
		_ = AppendWAL("put", "b", "k", nil)
	}

	count := 0
	err := ScanWAL(2, 0, func(e *WALEntry) bool {
		if e.Seq <= 2 {
			t.Fatalf("got entry seq=%d which is <=2", e.Seq)
		}
		count++
		return true
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 3 {
		t.Fatalf("count=%d want 3", count)
	}
}

func TestWALCursor(t *testing.T) {
	setupWALDB(t)
	if v, _ := GetWALCursor("a"); v != 0 {
		t.Fatalf("initial cursor=%d", v)
	}
	if err := SetWALCursor("a", 42); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, _ := GetWALCursor("a"); v != 42 {
		t.Fatalf("after set cursor=%d", v)
	}
}

func TestPurgeOldWAL(t *testing.T) {
	setupWALDB(t)

	if err := AppendWAL("put", "b", "k", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := AppendWAL("put", "b", "k2", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	purged, err := PurgeOldWAL(20 * time.Millisecond)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 1 {
		t.Fatalf("purged=%d want 1", purged)
	}
}

func TestApplyWALEntryPut(t *testing.T) {
	setupWALDB(t)

	e := &WALEntry{Op: "put", Bucket: "b", Key: "k", Value: []byte("hello")}
	if err := applyWALEntry(e); err != nil {
		t.Fatalf("apply: %v", err)
	}

	got, err := (struct{ s string }{}).s, error(nil)
	_ = got
	_ = err

	data, derr := (LocalMetaForTest{}).Get("b", "k")
	if derr != nil {
		t.Fatalf("get: %v", derr)
	}
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
}

type LocalMetaForTest struct{}

func (LocalMetaForTest) Get(bucket, key string) ([]byte, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, nil
	}
	k := []byte("metadata:" + bucket + "/" + key)
	return db.Get(k)
}
