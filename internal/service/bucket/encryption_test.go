package bucket

import (
	"errors"
	"testing"

	"github.com/sowahq/hangar/internal/testutil"
)

func TestEncryptionPutGetDelete(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "encb"}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	tests := []struct {
		name string
		cfg  *EncryptionConfig
	}{
		{name: "aes256-only", cfg: &EncryptionConfig{Algorithm: "AES256"}},
		{name: "aes256-with-kms-id", cfg: &EncryptionConfig{Algorithm: "AES256", KMSKeyID: "key-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PutEncryption("encb", tt.cfg); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, err := GetEncryption("encb")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Algorithm != tt.cfg.Algorithm || got.KMSKeyID != tt.cfg.KMSKeyID {
				t.Fatalf("got %+v want %+v", got, tt.cfg)
			}
		})
	}

	if err := DeleteEncryption("encb"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := GetEncryption("encb"); !errors.Is(err, ErrEncryptionNotFound) {
		t.Fatalf("expected ErrEncryptionNotFound, got %v", err)
	}
}

func TestEncryptionPutOnMissingBucket(t *testing.T) {
	testutil.SetupDB(t)

	err := PutEncryption("nope", &EncryptionConfig{Algorithm: "AES256"})
	if err == nil {
		t.Fatalf("expected error for missing bucket")
	}
}

func TestEncryptionCleanedOnBucketDelete(t *testing.T) {
	testutil.SetupDB(t)

	if _, err := CreateBucket(&CreateBucketRequest{Name: "encdel"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := PutEncryption("encdel", &EncryptionConfig{Algorithm: "AES256"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := DeleteBucket(&DeleteBucketRequest{Name: "encdel"}); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	if _, err := GetEncryption("encdel"); !errors.Is(err, ErrEncryptionNotFound) {
		t.Fatalf("expected encryption cleaned, got %v", err)
	}
}
