package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, opts Options) string {
	t.Helper()

	dir := t.TempDir()
	if opts.Path == "" {
		opts.Path = filepath.Join(dir, "audit.log")
	}
	opts.Enabled = true

	if err := Init(opts); err != nil {
		t.Fatalf("Init: %v", err)
	}

	t.Cleanup(func() {
		_ = Close()
		SetClockForTest(nil)
	})

	return opts.Path
}

func TestRecordAndTail(t *testing.T) {
	cases := []struct {
		name   string
		events []Event
		limit  int
		want   int
	}{
		{
			name: "single event",
			events: []Event{
				{Actor: "admin", ActorType: ActorTypeAdmin, Action: "bucket.create", Target: "x"},
			},
			limit: 10,
			want:  1,
		},
		{
			name: "multiple events tail",
			events: []Event{
				{ActorType: ActorTypeAdmin, Action: "a"},
				{ActorType: ActorTypeAdmin, Action: "b"},
				{ActorType: ActorTypeAdmin, Action: "c"},
			},
			limit: 2,
			want:  2,
		},
		{
			name:   "no events",
			events: nil,
			limit:  10,
			want:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup(t, Options{})

			for _, ev := range tc.events {
				Record(ev)
			}

			got, err := Tail(tc.limit)
			if err != nil {
				t.Fatalf("Tail: %v", err)
			}

			if len(got) != tc.want {
				t.Fatalf("got %d events, want %d", len(got), tc.want)
			}
		})
	}
}

func TestDisabledNoop(t *testing.T) {
	_ = Close()

	if err := Init(Options{Enabled: false}); err != nil {
		t.Fatalf("Init disabled: %v", err)
	}

	Record(Event{Action: "noop"})

	if _, err := Tail(10); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestRotation(t *testing.T) {
	path := setup(t, Options{MaxSizeBytes: 200, MaxBackups: 3})

	for i := 0; i < 20; i++ {
		Record(Event{
			ActorType: ActorTypeAdmin,
			Action:    "rotate.test",
			Target:    strings.Repeat("x", 40),
		})
	}

	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var backups int
	for _, e := range entries {
		if e.Name() == "audit.log" {
			continue
		}
		if strings.HasPrefix(e.Name(), "audit.log.") {
			backups++
		}
	}

	if backups == 0 {
		t.Fatalf("expected rotation backups, got none")
	}
	if backups > 3 {
		t.Fatalf("expected ≤3 backups (MaxBackups), got %d", backups)
	}
}

func TestRecordErrorResult(t *testing.T) {
	setup(t, Options{})

	Record(Event{
		ActorType: ActorTypeAdmin,
		Action:    "fail.case",
		Result:    ResultError,
		Error:     "boom",
	})

	got, err := Tail(10)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
	if got[0].Result != ResultError || got[0].Error != "boom" {
		t.Fatalf("unexpected event: %+v", got[0])
	}
}

func TestTSAutoFill(t *testing.T) {
	setup(t, Options{})

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	SetClockForTest(func() time.Time { return fixed })

	Record(Event{ActorType: ActorTypeSystem, Action: "tick"})

	got, err := Tail(1)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].TS != fixed.UnixMilli() {
		t.Fatalf("want TS=%d, got %d", fixed.UnixMilli(), got[0].TS)
	}
}
