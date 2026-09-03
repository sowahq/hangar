package sse

import (
	"errors"

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

	return crypto.EnvelopeSeal(kek, plain, nil)
}

func unwrapKeyBytes(blob []byte) ([]byte, error) {
	kek, err := keyringKEK()
	if err != nil {
		return nil, err
	}

	return crypto.EnvelopeOpen(kek, blob, nil)
}
