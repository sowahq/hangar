package object

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/crypto"
)

const (
	SSEAlgoNone = ""
	SSEAlgoS3   = "AES256"
	SSEAlgoC    = "AES256-C"

	hkdfInfoS3 = "hangar-sse-s3"
)

var (
	ErrSSEMasterKeyMissing       = errors.New("sse-s3 disabled: master key not configured")
	ErrSSECustomerKeyRequired    = errors.New("sse-c headers required")
	ErrSSECustomerKeyInvalid     = errors.New("sse-c key invalid")
	ErrSSECustomerKeyMD5Mismatch = errors.New("sse-c key md5 mismatch")
	ErrSSEAlgorithmInvalid       = errors.New("sse algorithm invalid")
	ErrSSECustomerOnUnencrypted  = errors.New("sse-c headers provided for unencrypted object")
	ErrSSECustomerForS3Object    = errors.New("sse-c headers provided for sse-s3 object")
)

type SSERequest struct {
	Algorithm        string
	CustomerKey      []byte
	CustomerKeyMD5   string
}

func ParseCustomerKey(b64Key, md5Header string) ([]byte, string, error) {
	if b64Key == "" || md5Header == "" {
		return nil, "", ErrSSECustomerKeyRequired
	}

	key, err := base64.StdEncoding.DecodeString(b64Key)
	if err != nil || len(key) != 32 {
		return nil, "", ErrSSECustomerKeyInvalid
	}

	sum := md5.Sum(key)
	expected := base64.StdEncoding.EncodeToString(sum[:])
	if expected != md5Header {
		return nil, "", ErrSSECustomerKeyMD5Mismatch
	}

	return key, md5Header, nil
}

func NewSSEParams(req *SSERequest) (key []byte, salt, noncePrefix []byte, customerMD5 string, err error) {
	switch req.Algorithm {
	case SSEAlgoNone:
		return nil, nil, nil, "", nil

	case SSEAlgoS3:
		master := config.MasterKey()
		if len(master) == 0 {
			return nil, nil, nil, "", ErrSSEMasterKeyMissing
		}

		salt = make([]byte, crypto.SaltSize)
		if _, err = rand.Read(salt); err != nil {
			return nil, nil, nil, "", fmt.Errorf("salt rand: %w", err)
		}

		noncePrefix = make([]byte, crypto.NoncePrefixLen)
		if _, err = rand.Read(noncePrefix); err != nil {
			return nil, nil, nil, "", fmt.Errorf("nonce rand: %w", err)
		}

		key, err = crypto.DeriveKey(master, salt, []byte(hkdfInfoS3))
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("derive: %w", err)
		}

		return key, salt, noncePrefix, "", nil

	case SSEAlgoC:
		if len(req.CustomerKey) != crypto.KeySize {
			return nil, nil, nil, "", ErrSSECustomerKeyInvalid
		}

		noncePrefix = make([]byte, crypto.NoncePrefixLen)
		if _, err = rand.Read(noncePrefix); err != nil {
			return nil, nil, nil, "", fmt.Errorf("nonce rand: %w", err)
		}

		key = make([]byte, crypto.KeySize)
		copy(key, req.CustomerKey)

		return key, nil, noncePrefix, req.CustomerKeyMD5, nil
	}

	return nil, nil, nil, "", ErrSSEAlgorithmInvalid
}

type ssePutCtx struct {
	algo           string
	salt           []byte
	noncePrefix    []byte
	customerKeyMD5 string
	encParams      *storage.EncryptParams
}

func setupSSEWrite(req *SSERequest) (*ssePutCtx, error) {
	if req == nil || req.Algorithm == SSEAlgoNone {
		return &ssePutCtx{algo: SSEAlgoNone}, nil
	}

	key, salt, np, md5sum, err := NewSSEParams(req)
	if err != nil {
		return nil, err
	}

	return &ssePutCtx{
		algo:           req.Algorithm,
		salt:           salt,
		noncePrefix:    np,
		customerKeyMD5: md5sum,
		encParams:      &storage.EncryptParams{Key: key, NoncePrefix: np, PartNumber: 1},
	}, nil
}

func ResolveReadKey(m *storage.Metadatas, req *SSERequest) ([]byte, error) {
	switch m.SSEAlgorithm {
	case SSEAlgoNone:
		if req != nil && req.Algorithm == SSEAlgoC {
			return nil, ErrSSECustomerOnUnencrypted
		}
		return nil, nil

	case SSEAlgoS3:
		if req != nil && req.Algorithm == SSEAlgoC {
			return nil, ErrSSECustomerForS3Object
		}

		master := config.MasterKey()
		if len(master) == 0 {
			return nil, ErrSSEMasterKeyMissing
		}

		return crypto.DeriveKey(master, m.SSESalt, []byte(hkdfInfoS3))

	case SSEAlgoC:
		if req == nil || req.Algorithm != SSEAlgoC {
			return nil, ErrSSECustomerKeyRequired
		}
		if req.CustomerKeyMD5 != m.SSECustomerKeyMD5 {
			return nil, ErrSSECustomerKeyMD5Mismatch
		}
		if len(req.CustomerKey) != crypto.KeySize {
			return nil, ErrSSECustomerKeyInvalid
		}

		key := make([]byte, crypto.KeySize)
		copy(key, req.CustomerKey)
		return key, nil
	}

	return nil, ErrSSEAlgorithmInvalid
}

func uploadPartEncryptParams(h *storage.MultipartHeader, req *UploadPartRequest) (*storage.EncryptParams, error) {
	switch h.SSEAlgorithm {
	case SSEAlgoNone:
		return nil, nil

	case SSEAlgoS3:
		master := config.MasterKey()
		if len(master) == 0 {
			return nil, ErrSSEMasterKeyMissing
		}
		key, err := crypto.DeriveKey(master, h.SSESalt, []byte(hkdfInfoS3))
		if err != nil {
			return nil, fmt.Errorf("derive: %w", err)
		}
		return &storage.EncryptParams{Key: key, NoncePrefix: h.SSENoncePrefix, PartNumber: uint16(req.PartNumber)}, nil

	case SSEAlgoC:
		if req.SSE == nil || req.SSE.Algorithm != SSEAlgoC {
			return nil, ErrSSECustomerKeyRequired
		}
		if req.SSE.CustomerKeyMD5 != h.SSECustomerKeyMD5 {
			return nil, ErrSSECustomerKeyMD5Mismatch
		}
		key := make([]byte, crypto.KeySize)
		copy(key, req.SSE.CustomerKey)
		return &storage.EncryptParams{Key: key, NoncePrefix: h.SSENoncePrefix, PartNumber: uint16(req.PartNumber)}, nil
	}

	return nil, ErrSSEAlgorithmInvalid
}

func chunkPartLookup(m *storage.Metadatas, globalIdx int) (partNumber uint16, localIdx uint64) {
	if len(m.SSEPartChunkCounts) == 0 {
		return 1, uint64(globalIdx)
	}

	remaining := globalIdx
	for i, count := range m.SSEPartChunkCounts {
		if remaining < count {
			return uint16(m.SSEPartNumbers[i]), uint64(remaining)
		}
		remaining -= count
	}

	last := len(m.SSEPartChunkCounts) - 1
	return uint16(m.SSEPartNumbers[last]), uint64(remaining)
}
