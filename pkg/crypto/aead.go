package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	KeySize        = 32
	NonceSize      = 12
	TagSize        = 16
	NoncePrefixLen = 4
	SaltSize       = 16
)

var (
	ErrKeySize        = errors.New("crypto: key must be 32 bytes")
	ErrNonceSize      = errors.New("crypto: nonce must be 12 bytes")
	ErrNoncePrefixLen = errors.New("crypto: nonce prefix must be 4 bytes")
	ErrCiphertextLen  = errors.New("crypto: ciphertext shorter than auth tag")
	ErrAuthFailed     = errors.New("crypto: AEAD authentication failed")
)

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	return aead, nil
}

func Seal(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, ErrNonceSize
	}

	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	return aead.Seal(nil, nonce, plaintext, aad), nil
}

func Open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, ErrNonceSize
	}
	if len(ciphertext) < TagSize {
		return nil, ErrCiphertextLen
	}

	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	pt, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrAuthFailed
	}

	return pt, nil
}

func EnvelopeSeal(key, plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ct, err := Seal(key, nonce, plaintext, aad)
	if err != nil {
		return nil, err
	}

	return append(nonce, ct...), nil
}

func EnvelopeOpen(key, blob, aad []byte) ([]byte, error) {
	if len(blob) < NonceSize {
		return nil, ErrCiphertextLen
	}

	return Open(key, blob[:NonceSize], blob[NonceSize:], aad)
}

func DeriveKey(master, salt, info []byte) ([]byte, error) {
	if len(master) != KeySize {
		return nil, ErrKeySize
	}

	r := hkdf.New(sha256.New, master, salt, info)
	out := make([]byte, KeySize)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}

	return out, nil
}

func ChunkNonce(prefix []byte, idx uint64) ([]byte, error) {
	if len(prefix) != NoncePrefixLen {
		return nil, ErrNoncePrefixLen
	}

	nonce := make([]byte, NonceSize)
	copy(nonce[:NoncePrefixLen], prefix)
	binary.BigEndian.PutUint64(nonce[NoncePrefixLen:], idx)

	return nonce, nil
}
