package sse

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/pkg/crypto"
)

var ErrKeyringMasterMissing = errors.New("sse keyring: master key not configured")

const (
	keyringKEKSalt = "hangar-sse-keyring"
	keyringKEKInfo = "hangar-sse-keyring-kek"
)

func keyringKEK() ([]byte, error) {
	master := config.MasterKey()
	if len(master) != crypto.KeySize {
		return nil, ErrKeyringMasterMissing
	}
	return crypto.DeriveKey(master, []byte(keyringKEKSalt), []byte(keyringKEKInfo))
}

func wrapKeyBytes(plain []byte) ([]byte, error) {
	kek, err := keyringKEK()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, crypto.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ct, err := crypto.Seal(kek, nonce, plain, nil)
	if err != nil {
		return nil, err
	}

	return append(nonce, ct...), nil
}

func unwrapKeyBytes(blob []byte) ([]byte, error) {
	kek, err := keyringKEK()
	if err != nil {
		return nil, err
	}

	if len(blob) < crypto.NonceSize {
		return nil, fmt.Errorf("sse keyring: wrapped key too short")
	}

	return crypto.Open(kek, blob[:crypto.NonceSize], blob[crypto.NonceSize:], nil)
}
