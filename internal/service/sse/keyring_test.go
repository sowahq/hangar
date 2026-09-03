package sse

import (
	"bytes"
	"testing"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/database"
	"github.com/sowahq/hangar/internal/testutil"
)

func TestKeyringBootstrapAndRotate(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T)
	}{
		{
			name: "bootstrap-seed-default",
			fn: func(t *testing.T) {
				master := bytes.Repeat([]byte{1}, 32)
				if err := Bootstrap(master); err != nil {
					t.Fatalf("bootstrap: %v", err)
				}
				id, k, err := ActiveKey()
				if err != nil {
					t.Fatalf("active: %v", err)
				}
				if id != defaultKeyID {
					t.Fatalf("id=%q", id)
				}
				if !bytes.Equal(k, master) {
					t.Fatal("master mismatch")
				}
			},
		},
		{
			name: "bootstrap-empty-noop",
			fn: func(t *testing.T) {
				if err := Bootstrap(nil); err != nil {
					t.Fatalf("bootstrap: %v", err)
				}
				if _, _, err := ActiveKey(); err == nil {
					t.Fatal("expected no active key")
				}
			},
		},
		{
			name: "rotate-keeps-old-key-readable",
			fn: func(t *testing.T) {
				master := bytes.Repeat([]byte{7}, 32)
				if err := Bootstrap(master); err != nil {
					t.Fatalf("bootstrap: %v", err)
				}
				newID, err := Rotate()
				if err != nil {
					t.Fatalf("rotate: %v", err)
				}
				if newID == defaultKeyID {
					t.Fatal("expected new id")
				}
				id, _, err := ActiveKey()
				if err != nil || id != newID {
					t.Fatalf("active=%q err=%v", id, err)
				}
				old, err := KeyBytes(defaultKeyID)
				if err != nil || !bytes.Equal(old, master) {
					t.Fatalf("default key lost: err=%v", err)
				}
				keys, err := List()
				if err != nil || len(keys) != 2 {
					t.Fatalf("list err=%v len=%d", err, len(keys))
				}
			},
		},
		{
			name: "set-active-unknown",
			fn: func(t *testing.T) {
				master := bytes.Repeat([]byte{3}, 32)
				if err := Bootstrap(master); err != nil {
					t.Fatalf("bootstrap: %v", err)
				}
				if err := SetActive("does-not-exist"); err == nil {
					t.Fatal("expected error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.SetupDB(t)
			config.SetMasterKeyForTest(bytes.Repeat([]byte{9}, 32))
			t.Cleanup(func() { config.SetMasterKeyForTest(nil) })
			tt.fn(t)
		})
	}
}

func TestKeyringStoredCiphertext(t *testing.T) {
	testutil.SetupDB(t)
	config.SetMasterKeyForTest(bytes.Repeat([]byte{9}, 32))
	t.Cleanup(func() { config.SetMasterKeyForTest(nil) })

	master := bytes.Repeat([]byte{3}, 32)
	if err := Bootstrap(master); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	raw, err := database.LocalStore().Get([]byte(keyPrefix + defaultKeyID))
	if err != nil {
		t.Fatalf("get raw: %v", err)
	}
	if bytes.Contains(raw, master) {
		t.Fatal("keyring stored the raw key material in cleartext")
	}

	_, k, err := ActiveKey()
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if !bytes.Equal(k, master) {
		t.Fatal("unwrapped key does not match the master")
	}
}
