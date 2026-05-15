package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func mustKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestSealOpenRoundtrip(t *testing.T) {
	cases := []struct {
		name      string
		plaintext []byte
		aad       []byte
	}{
		{"empty", []byte{}, nil},
		{"short", []byte("hello"), nil},
		{"with-aad", []byte("payload"), []byte("aad-context")},
		{"binary", bytes.Repeat([]byte{0xAB}, 4096), []byte("blob")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := mustKey(t)
			nonce := make([]byte, NonceSize)
			if _, err := rand.Read(nonce); err != nil {
				t.Fatalf("rand: %v", err)
			}

			ct, err := Seal(key, nonce, tc.plaintext, tc.aad)
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if len(ct) != len(tc.plaintext)+TagSize {
				t.Fatalf("ciphertext len=%d want %d", len(ct), len(tc.plaintext)+TagSize)
			}

			pt, err := Open(key, nonce, ct, tc.aad)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !bytes.Equal(pt, tc.plaintext) {
				t.Fatalf("plaintext mismatch")
			}
		})
	}
}

func TestSealOpenErrors(t *testing.T) {
	key := mustKey(t)
	nonce := make([]byte, NonceSize)
	rand.Read(nonce)

	ct, err := Seal(key, nonce, []byte("data"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cases := []struct {
		name string
		fn   func() error
		want error
	}{
		{
			"bad-key-size",
			func() error {
				_, err := Seal(make([]byte, 16), nonce, []byte("x"), nil)
				return err
			},
			ErrKeySize,
		},
		{
			"bad-nonce-size",
			func() error {
				_, err := Seal(key, make([]byte, 8), []byte("x"), nil)
				return err
			},
			ErrNonceSize,
		},
		{
			"open-wrong-key",
			func() error {
				_, err := Open(mustKey(t), nonce, ct, []byte("aad"))
				return err
			},
			ErrAuthFailed,
		},
		{
			"open-wrong-aad",
			func() error {
				_, err := Open(key, nonce, ct, []byte("nope"))
				return err
			},
			ErrAuthFailed,
		},
		{
			"open-flipped-bit",
			func() error {
				tampered := append([]byte(nil), ct...)
				tampered[0] ^= 1
				_, err := Open(key, nonce, tampered, []byte("aad"))
				return err
			},
			ErrAuthFailed,
		},
		{
			"open-short-ciphertext",
			func() error {
				_, err := Open(key, nonce, []byte{1, 2, 3}, nil)
				return err
			},
			ErrCiphertextLen,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}

func TestDeriveKey(t *testing.T) {
	master := mustKey(t)
	salt := []byte("salt-bytes")
	info := []byte("hangar-sse-s3")

	k1, err := DeriveKey(master, salt, info)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if len(k1) != KeySize {
		t.Fatalf("len=%d want %d", len(k1), KeySize)
	}

	k2, err := DeriveKey(master, salt, info)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Fatalf("derivation not deterministic")
	}

	k3, _ := DeriveKey(master, []byte("other-salt"), info)
	if bytes.Equal(k1, k3) {
		t.Fatalf("salt change did not alter key")
	}

	k4, _ := DeriveKey(master, salt, []byte("other-info"))
	if bytes.Equal(k1, k4) {
		t.Fatalf("info change did not alter key")
	}

	if _, err := DeriveKey(make([]byte, 16), salt, info); !errors.Is(err, ErrKeySize) {
		t.Fatalf("want ErrKeySize, got %v", err)
	}
}

func TestChunkNonce(t *testing.T) {
	prefix := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	n0, err := ChunkNonce(prefix, 0)
	if err != nil {
		t.Fatalf("ChunkNonce: %v", err)
	}
	if len(n0) != NonceSize {
		t.Fatalf("len=%d want %d", len(n0), NonceSize)
	}
	if !bytes.Equal(n0[:NoncePrefixLen], prefix) {
		t.Fatalf("prefix not preserved")
	}

	n1, _ := ChunkNonce(prefix, 1)
	if bytes.Equal(n0, n1) {
		t.Fatalf("nonce idx 0 == idx 1")
	}

	nMax, _ := ChunkNonce(prefix, 0xFFFFFFFFFFFFFFFF)
	wantTail := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(nMax[NoncePrefixLen:], wantTail) {
		t.Fatalf("big-endian encoding wrong")
	}

	if _, err := ChunkNonce([]byte{1, 2, 3}, 0); !errors.Is(err, ErrNoncePrefixLen) {
		t.Fatalf("want ErrNoncePrefixLen, got %v", err)
	}
}
