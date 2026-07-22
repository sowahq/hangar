package lifecycle

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/internal/testutil"
)

func putOld(t *testing.T, name, key string, ageDays int) {
	t.Helper()
	if _, err := object.PutObject(&object.PutObjectRequest{
		Bucket: name, Key: key,
		Body: bytes.NewReader([]byte("x")),
	}); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
	m, err := object.GetMetadata(name, key)
	if err != nil {
		t.Fatalf("get meta %s: %v", key, err)
	}
	m.CreatedAt = time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour).UnixMilli()
	if err := storage.StoreMetadataInBucket(name, m); err != nil {
		t.Fatalf("store meta: %v", err)
	}
}

func TestLifecycleRun(t *testing.T) {
	tests := []struct {
		name        string
		rules       []bucket.LifecycleRule
		seeds       map[string]int
		wantExpired int
		wantSurvive []string
	}{
		{
			name: "expires-by-prefix",
			rules: []bucket.LifecycleRule{
				{ID: "tmp", Enabled: true, Prefix: "tmp/", ExpirationDays: 7},
			},
			seeds:       map[string]int{"tmp/a": 10, "tmp/b": 2, "keep/c": 30},
			wantExpired: 1,
			wantSurvive: []string{"tmp/b", "keep/c"},
		},
		{
			name: "disabled-rule-noop",
			rules: []bucket.LifecycleRule{
				{ID: "x", Enabled: false, Prefix: "", ExpirationDays: 1},
			},
			seeds:       map[string]int{"a": 30},
			wantExpired: 0,
			wantSurvive: []string{"a"},
		},
		{
			name: "longest-prefix-wins",
			rules: []bucket.LifecycleRule{
				{ID: "all", Enabled: true, Prefix: "", ExpirationDays: 1},
				{ID: "keep-logs", Enabled: true, Prefix: "logs/", ExpirationDays: 365},
			},
			seeds:       map[string]int{"data": 30, "logs/2026": 30},
			wantExpired: 1,
			wantSurvive: []string{"logs/2026"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupServer(t)
			if _, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: "lcb"}); err != nil {
				t.Fatalf("create: %v", err)
			}
			if err := bucket.PutLifecycle("lcb", &bucket.LifecycleConfiguration{Rules: tt.rules}); err != nil {
				t.Fatalf("put cfg: %v", err)
			}
			for k, age := range tt.seeds {
				putOld(t, "lcb", k, age)
			}

			stats, err := Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if stats.ObjectsExpired != tt.wantExpired {
				t.Fatalf("expired=%d want=%d", stats.ObjectsExpired, tt.wantExpired)
			}
			for _, k := range tt.wantSurvive {
				if _, err := object.GetMetadata("lcb", k); err != nil {
					t.Fatalf("survivor %q missing: %v", k, err)
				}
			}
		})
	}
}

func TestMatchLifecycleRule(t *testing.T) {
	cfg := &bucket.LifecycleConfiguration{Rules: []bucket.LifecycleRule{
		{Enabled: true, Prefix: "", ExpirationDays: 1},
		{Enabled: true, Prefix: "logs/", ExpirationDays: 30},
		{Enabled: false, Prefix: "skip/", ExpirationDays: 1},
		{Enabled: true, Prefix: "noop/", ExpirationDays: 0},
	}}

	tests := []struct {
		key       string
		wantDays  int
		wantMatch bool
	}{
		{key: "x", wantDays: 1, wantMatch: true},
		{key: "logs/2026", wantDays: 30, wantMatch: true},
		{key: "skip/y", wantDays: 1, wantMatch: true},
		{key: "noop/z", wantDays: 1, wantMatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			r := bucket.MatchLifecycleRule(cfg, tt.key)
			if tt.wantMatch && r == nil {
				t.Fatalf("expected match")
			}
			if r != nil && r.ExpirationDays != tt.wantDays {
				t.Fatalf("days=%d want=%d", r.ExpirationDays, tt.wantDays)
			}
		})
	}
}
