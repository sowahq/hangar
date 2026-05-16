package diskspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anhostfr/hangar/internal/config"
)

func setDataDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	cfg := config.ServerConfig()
	prev := cfg.DataDirectory
	cfg.DataDirectory = dir

	t.Cleanup(func() {
		cfg.DataDirectory = prev
		config.SetDiskSafeguardForTest(0, 0, 0)
		InvalidateUsageCache()
	})

	return dir
}

func TestCheckMinFreeBytes(t *testing.T) {
	dir := setDataDir(t)
	_ = dir

	cases := []struct {
		name      string
		minFree   int64
		extra     int64
		wantBlock bool
	}{
		{name: "disabled", minFree: 0, extra: 0, wantBlock: false},
		{name: "huge threshold blocks", minFree: 1 << 62, extra: 0, wantBlock: true},
		{name: "huge threshold blocks with extra", minFree: 1 << 60, extra: 1 << 60, wantBlock: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config.SetDiskSafeguardForTest(tc.minFree, 0, 0)
			InvalidateUsageCache()

			err := Check(tc.extra)
			gotBlock := errors.Is(err, ErrInsufficientStorage)

			if gotBlock != tc.wantBlock {
				t.Fatalf("Check(%d) min_free=%d: block=%v want=%v err=%v", tc.extra, tc.minFree, gotBlock, tc.wantBlock, err)
			}
		})
	}
}

func TestCheckMinFreePct(t *testing.T) {
	setDataDir(t)

	cases := []struct {
		name      string
		minPct    int
		wantBlock bool
	}{
		{name: "disabled", minPct: 0, wantBlock: false},
		{name: "require 99 pct free blocks on used disk", minPct: 99, wantBlock: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config.SetDiskSafeguardForTest(0, tc.minPct, 0)
			InvalidateUsageCache()

			err := Check(0)
			gotBlock := errors.Is(err, ErrInsufficientStorage)

			if gotBlock != tc.wantBlock {
				t.Fatalf("Check min_free_pct=%d: block=%v want=%v err=%v", tc.minPct, gotBlock, tc.wantBlock, err)
			}
		})
	}
}

func TestCheckNodeMaxBytes(t *testing.T) {
	dir := setDataDir(t)

	if err := os.WriteFile(filepath.Join(dir, "blob.dat"), make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	cases := []struct {
		name      string
		nodeMax   int64
		extra     int64
		wantBlock bool
	}{
		{name: "disabled", nodeMax: 0, extra: 0, wantBlock: false},
		{name: "high cap allows", nodeMax: 1 << 30, extra: 0, wantBlock: false},
		{name: "tight cap blocks", nodeMax: 512, extra: 0, wantBlock: true},
		{name: "exact cap with extra blocks", nodeMax: 1024, extra: 1, wantBlock: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config.SetDiskSafeguardForTest(0, 0, tc.nodeMax)
			InvalidateUsageCache()

			err := Check(tc.extra)
			gotBlock := errors.Is(err, ErrInsufficientStorage)

			if gotBlock != tc.wantBlock {
				t.Fatalf("Check(extra=%d) node_max=%d: block=%v want=%v err=%v", tc.extra, tc.nodeMax, gotBlock, tc.wantBlock, err)
			}
		})
	}
}

func TestSnapshotShape(t *testing.T) {
	setDataDir(t)
	config.SetDiskSafeguardForTest(123, 5, 4096)
	InvalidateUsageCache()

	s := Snapshot()

	if s.MinFreeBytes != 123 || s.MinFreePct != 5 || s.NodeMaxBytes != 4096 {
		t.Fatalf("snapshot config fields wrong: %+v", s)
	}

	if s.DataPath == "" {
		t.Fatalf("snapshot data path empty")
	}
}
