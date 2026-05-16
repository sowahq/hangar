package bucket

import (
	"errors"
	"testing"

	"github.com/anhostfr/hangar/internal/testutil"
)

func TestObjectLockRequiresVersioning(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "olb"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := PutObjectLockConfig("olb", &ObjectLockConfig{Enabled: true})
	if !errors.Is(err, ErrObjectLockNeedsVersion) {
		t.Fatalf("expected ErrObjectLockNeedsVersion, got %v", err)
	}
}

func TestObjectLockPutGetWithVersioning(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "olb"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := UpdateVersioning("olb", true); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}

	tests := []struct {
		name string
		cfg  *ObjectLockConfig
	}{
		{name: "enabled-no-default", cfg: &ObjectLockConfig{Enabled: true}},
		{
			name: "enabled-with-governance-30d",
			cfg: &ObjectLockConfig{
				Enabled:          true,
				DefaultRetention: &DefaultRetention{Mode: ObjectLockModeGovernance, Days: 30},
			},
		},
		{
			name: "enabled-with-compliance-1y",
			cfg: &ObjectLockConfig{
				Enabled:          true,
				DefaultRetention: &DefaultRetention{Mode: ObjectLockModeCompliance, Years: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PutObjectLockConfig("olb", tt.cfg); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, err := GetObjectLockConfig("olb")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if !got.Enabled {
				t.Fatalf("expected enabled")
			}
			if (got.DefaultRetention == nil) != (tt.cfg.DefaultRetention == nil) {
				t.Fatalf("retention mismatch: got %+v want %+v", got.DefaultRetention, tt.cfg.DefaultRetention)
			}
			if got.DefaultRetention != nil && *got.DefaultRetention != *tt.cfg.DefaultRetention {
				t.Fatalf("retention mismatch: got %+v want %+v", got.DefaultRetention, tt.cfg.DefaultRetention)
			}
		})
	}

	info, err := GetBucket("olb")
	if err != nil {
		t.Fatalf("get bucket: %v", err)
	}
	if !info.ObjectLockEnabled {
		t.Fatalf("bucket flag not flipped")
	}
}

func TestObjectLockInvalidRetention(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "olb"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := UpdateVersioning("olb", true); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}

	tests := []struct {
		name string
		cfg  *ObjectLockConfig
		want error
	}{
		{
			name: "bad-mode",
			cfg: &ObjectLockConfig{
				Enabled:          true,
				DefaultRetention: &DefaultRetention{Mode: "EVIL", Days: 1},
			},
			want: ErrObjectLockInvalidMode,
		},
		{
			name: "zero-days-and-years",
			cfg: &ObjectLockConfig{
				Enabled:          true,
				DefaultRetention: &DefaultRetention{Mode: ObjectLockModeGovernance},
			},
			want: ErrObjectLockInvalidRetain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PutObjectLockConfig("olb", tt.cfg); !errors.Is(err, tt.want) {
				t.Fatalf("got %v want %v", err, tt.want)
			}
		})
	}
}

func TestObjectLockCleanedOnBucketDelete(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "oldel"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := UpdateVersioning("oldel", true); err != nil {
		t.Fatalf("versioning: %v", err)
	}
	if err := PutObjectLockConfig("oldel", &ObjectLockConfig{Enabled: true}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := DeleteBucket(&DeleteBucketRequest{Name: "oldel"}); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	if _, err := GetObjectLockConfig("oldel"); !errors.Is(err, ErrObjectLockNotConfigured) {
		t.Fatalf("expected cleaned, got %v", err)
	}
}
