package auth

import (
	"errors"
	"testing"

	"github.com/sowahq/hangar/internal/testutil"
)

func TestCreateS3Key(t *testing.T) {
	tests := []struct {
		name    string
		perms   []string
		buckets []string
		wantErr bool
	}{
		{name: "single perm all buckets", perms: []string{"read"}, buckets: nil, wantErr: false},
		{name: "multi perms scoped", perms: []string{"read", "write"}, buckets: []string{"b1", "b2"}, wantErr: false},
		{name: "admin", perms: []string{"admin"}, buckets: nil, wantErr: false},
		{name: "no perms", perms: nil, buckets: nil, wantErr: true},
		{name: "invalid perm", perms: []string{"hack"}, buckets: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupDB(t)
			k, err := CreateS3Key(tt.perms, tt.buckets)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateS3Key: %v", err)
			}
			if len(k.AccessKeyID) != accessKeyIDLen {
				t.Fatalf("access key id len=%d want=%d", len(k.AccessKeyID), accessKeyIDLen)
			}
			if k.SecretKey == "" {
				t.Fatalf("empty secret")
			}
			got, err := GetS3Key(k.AccessKeyID)
			if err != nil {
				t.Fatalf("GetS3Key: %v", err)
			}
			if got.SecretKey != k.SecretKey {
				t.Fatalf("secret mismatch")
			}
		})
	}
}

func TestS3KeyRevokeAndList(t *testing.T) {
	testutil.SetupDB(t)
	k1, err := CreateS3Key([]string{"read"}, nil)
	if err != nil {
		t.Fatalf("create k1: %v", err)
	}
	k2, err := CreateS3Key([]string{"write"}, []string{"b1"})
	if err != nil {
		t.Fatalf("create k2: %v", err)
	}

	list, err := ListS3Keys()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 got %d", len(list))
	}

	if err := RevokeS3Key(k1.AccessKeyID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := GetS3Key(k1.AccessKeyID); !errors.Is(err, ErrS3KeyNotFound) {
		t.Fatalf("get revoked err=%v want=ErrS3KeyNotFound", err)
	}
	if err := RevokeS3Key(k1.AccessKeyID); !errors.Is(err, ErrS3KeyNotFound) {
		t.Fatalf("double revoke err=%v want=ErrS3KeyNotFound", err)
	}

	if _, err := GetS3Key(k2.AccessKeyID); err != nil {
		t.Fatalf("k2 should still exist: %v", err)
	}
}

func TestS3KeyAllowsBucket(t *testing.T) {
	tests := []struct {
		name    string
		buckets []string
		bucket  string
		want    bool
	}{
		{name: "wildcard empty", buckets: nil, bucket: "any", want: true},
		{name: "match", buckets: []string{"a", "b"}, bucket: "b", want: true},
		{name: "miss", buckets: []string{"a", "b"}, bucket: "c", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &S3Key{Buckets: tt.buckets}
			if got := k.AllowsBucket(tt.bucket); got != tt.want {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestS3KeyHasPermission(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
		check string
		want  bool
	}{
		{name: "direct", perms: []string{"read", "write"}, check: "read", want: true},
		{name: "missing", perms: []string{"read"}, check: "delete", want: false},
		{name: "admin implies", perms: []string{"admin"}, check: "delete", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &S3Key{Permissions: tt.perms}
			if got := k.HasPermission(tt.check); got != tt.want {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}
