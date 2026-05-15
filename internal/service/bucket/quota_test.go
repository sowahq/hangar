package bucket

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/internal/testutil"
)

func writeMeta(t *testing.T, bucket, key string, size int64) {
	t.Helper()
	m := storage.Metadatas{Key: key, Size: size}
	data, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := database.LocalStore().Put([]byte(fmt.Sprintf("metadata:%s/%s", bucket, key)), data); err != nil {
		t.Fatalf("put meta: %v", err)
	}
}

func TestGetUsage(t *testing.T) {
	tests := []struct {
		name        string
		objects     []struct{ key string; size int64 }
		wantBytes   int64
		wantObjects int64
	}{
		{name: "empty", wantBytes: 0, wantObjects: 0},
		{name: "single", objects: []struct{ key string; size int64 }{{"a", 100}}, wantBytes: 100, wantObjects: 1},
		{name: "multiple", objects: []struct{ key string; size int64 }{{"a", 50}, {"b", 25}, {"c", 75}}, wantBytes: 150, wantObjects: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupDB(t)
			for _, o := range tt.objects {
				writeMeta(t, "qb", o.key, o.size)
			}
			b, n, err := GetUsage("qb")
			if err != nil {
				t.Fatalf("GetUsage: %v", err)
			}
			if b != tt.wantBytes || n != tt.wantObjects {
				t.Fatalf("got bytes=%d objects=%d want %d/%d", b, n, tt.wantBytes, tt.wantObjects)
			}
		})
	}
}

func TestUpdateQuota(t *testing.T) {
	tests := []struct {
		name       string
		maxBytes   int64
		maxObjects int64
		wantErr    bool
	}{
		{name: "set both", maxBytes: 1000, maxObjects: 10, wantErr: false},
		{name: "unlimited", maxBytes: 0, maxObjects: 0, wantErr: false},
		{name: "negative bytes", maxBytes: -1, maxObjects: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupDB(t)
			_, err := CreateBucket(&CreateBucketRequest{Name: "quotab"})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			_, err = UpdateQuota("quotab", tt.maxBytes, tt.maxObjects)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateQuota: %v", err)
			}
			info, err := GetBucket("quotab")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if info.MaxBytes != tt.maxBytes || info.MaxObjects != tt.maxObjects {
				t.Fatalf("got %d/%d want %d/%d", info.MaxBytes, info.MaxObjects, tt.maxBytes, tt.maxObjects)
			}
		})
	}
}
