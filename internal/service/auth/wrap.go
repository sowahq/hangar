package auth

import (
	"encoding/base64"
	"errors"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/pkg/crypto"
)

var ErrS3KeyMasterMissing = errors.New("s3 key: master key not configured, cannot decrypt secret at rest")

const (
	s3KeyKEKSalt = "hangar-s3key"
	s3KeyKEKInfo = "hangar-s3key-kek"
)

func s3SecretKEK() ([]byte, bool) {
	master := config.MasterKey()
	if len(master) != crypto.KeySize {
		return nil, false
	}

	kek, err := crypto.DeriveKey(master, []byte(s3KeyKEKSalt), []byte(s3KeyKEKInfo))
	if err != nil {
		return nil, false
	}

	return kek, true
}

func wrapSecret(secret string) (string, bool) {
	kek, ok := s3SecretKEK()
	if !ok {
		return secret, false
	}

	blob, err := crypto.EnvelopeSeal(kek, []byte(secret), nil)
	if err != nil {
		return secret, false
	}

	return base64.StdEncoding.EncodeToString(blob), true
}

func unwrapSecret(stored string) (string, error) {
	kek, ok := s3SecretKEK()
	if !ok {
		return "", ErrS3KeyMasterMissing
	}

	blob, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", err
	}

	plain, err := crypto.EnvelopeOpen(kek, blob, nil)
	if err != nil {
		return "", err
	}

	return string(plain), nil
}
