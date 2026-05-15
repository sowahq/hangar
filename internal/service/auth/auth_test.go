package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/anhostfr/hangar/internal/testutil"
)

func TestCreateAndVerifyToken(t *testing.T) {
	tests := []struct {
		name         string
		bucket       string
		perms        []string
		wantCreateOK bool
	}{
		{name: "single perm", bucket: "b1", perms: []string{"read"}, wantCreateOK: true},
		{name: "multi perms", bucket: "b2", perms: []string{"read", "write", "delete"}, wantCreateOK: true},
		{name: "admin perm", bucket: "b3", perms: []string{"admin"}, wantCreateOK: true},
		{name: "no perms", bucket: "b4", perms: nil, wantCreateOK: false},
		{name: "empty bucket", bucket: "", perms: []string{"read"}, wantCreateOK: false},
		{name: "invalid perm", bucket: "b5", perms: []string{"hack"}, wantCreateOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupDB(t)
			raw, tok, err := CreateToken(tt.bucket, tt.perms)
			if tt.wantCreateOK {
				if err != nil {
					t.Fatalf("CreateToken err: %v", err)
				}
				if raw == "" || tok == nil {
					t.Fatalf("empty raw or tok")
				}
				if !strings.Contains(raw, ".") {
					t.Fatalf("raw token missing dot: %s", raw)
				}
				v, err := VerifyToken(raw, tt.bucket, tt.perms[0])
				if err != nil {
					t.Fatalf("VerifyToken: %v", err)
				}
				if v.ID != tok.ID {
					t.Fatalf("id mismatch: %s vs %s", v.ID, tok.ID)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error")
				}
			}
		})
	}
}

func TestVerifyTokenErrors(t *testing.T) {
	testutil.SetupDB(t)
	raw, _, err := CreateToken("mybucket", []string{"read", "write"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name    string
		token   string
		bucket  string
		perm    string
		wantErr error
	}{
		{name: "ok", token: raw, bucket: "mybucket", perm: "read", wantErr: nil},
		{name: "wrong bucket", token: raw, bucket: "other", perm: "read", wantErr: ErrBucketMismatch},
		{name: "missing perm", token: raw, bucket: "mybucket", perm: "delete", wantErr: ErrNoPermission},
		{name: "malformed", token: "garbage", bucket: "mybucket", perm: "read", wantErr: ErrInvalidToken},
		{name: "unknown id", token: "abcdefghijkl.bogus", bucket: "mybucket", perm: "read", wantErr: ErrTokenNotFound},
		{name: "empty", token: "", bucket: "mybucket", perm: "read", wantErr: ErrInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifyToken(tt.token, tt.bucket, tt.perm)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err=%v want=%v", err, tt.wantErr)
			}
		})
	}
}

func TestRevokeAndList(t *testing.T) {
	testutil.SetupDB(t)
	raw, tok, err := CreateToken("rbucket", []string{"read"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := ListTokens("rbucket")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 got %d", len(list))
	}
	if err := RevokeToken(tok.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := VerifyToken(raw, "rbucket", "read"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("after revoke err=%v want=ErrTokenNotFound", err)
	}
	if err := RevokeToken(tok.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("revoke again err=%v want=ErrTokenNotFound", err)
	}
}

func TestAdminPermImpliesAll(t *testing.T) {
	testutil.SetupDB(t)
	raw, _, err := CreateToken("adb", []string{"admin"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, p := range []string{"read", "write", "delete"} {
		if _, err := VerifyToken(raw, "adb", p); err != nil {
			t.Fatalf("perm %s err=%v", p, err)
		}
	}
}
