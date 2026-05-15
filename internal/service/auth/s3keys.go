package auth

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anhostfr/hangar/internal/database"
	"github.com/cockroachdb/pebble"
)

var (
	ErrS3KeyNotFound = errors.New("s3 key not found")
	ErrS3KeyExists   = errors.New("s3 key already exists")
)

const (
	accessKeyIDLen  = 20
	secretKeyBytes  = 30
	s3KeyPrefix     = "s3key:"
)

type S3Key struct {
	AccessKeyID string   `json:"access_key_id"`
	SecretKey   string   `json:"secret_key"`
	Permissions []string `json:"permissions"`
	Buckets     []string `json:"buckets"`
	CreatedAt   int64    `json:"created_at"`
}

func s3KeyKey(id string) []byte {
	return []byte(s3KeyPrefix + id)
}

func generateAccessKeyID() (string, error) {
	raw := make([]byte, 13)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if len(enc) < accessKeyIDLen {
		return "", fmt.Errorf("access key id too short")
	}
	return enc[:accessKeyIDLen], nil
}

func generateSecretKey() (string, error) {
	raw := make([]byte, secretKeyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func validatePerms(perms []string) error {
	if len(perms) == 0 {
		return fmt.Errorf("at least one permission required")
	}
	for _, p := range perms {
		switch p {
		case PermRead, PermWrite, PermDelete, PermAdmin:
		default:
			return fmt.Errorf("invalid permission: %s", p)
		}
	}
	return nil
}

func CreateS3Key(perms, buckets []string) (*S3Key, error) {
	if err := validatePerms(perms); err != nil {
		return nil, err
	}
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	id, err := generateAccessKeyID()
	if err != nil {
		return nil, fmt.Errorf("gen access key: %w", err)
	}
	secret, err := generateSecretKey()
	if err != nil {
		return nil, fmt.Errorf("gen secret: %w", err)
	}

	if buckets == nil {
		buckets = []string{}
	}
	key := &S3Key{
		AccessKeyID: id,
		SecretKey:   secret,
		Permissions: perms,
		Buckets:     buckets,
		CreatedAt:   nowUnixMilli(),
	}
	data, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	if err := db.Put(s3KeyKey(id), data); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return key, nil
}

func GetS3Key(id string) (*S3Key, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get(s3KeyKey(id))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrS3KeyNotFound
		}
		return nil, err
	}
	var k S3Key
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &k, nil
}

func RevokeS3Key(id string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	exists, err := db.Exist(s3KeyKey(id))
	if err != nil {
		return err
	}
	if !exists {
		return ErrS3KeyNotFound
	}
	return db.Delete(s3KeyKey(id))
}

func ListS3Keys() ([]S3Key, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	iter, err := db.NewIteratorWithPrefix([]byte(s3KeyPrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	out := []S3Key{}
	for iter.First(); iter.Valid(); iter.Next() {
		var k S3Key
		if err := json.Unmarshal(iter.Value(), &k); err != nil {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

func (k *S3Key) AllowsBucket(bucket string) bool {
	if len(k.Buckets) == 0 {
		return true
	}
	for _, b := range k.Buckets {
		if b == bucket {
			return true
		}
	}
	return false
}

func (k *S3Key) HasPermission(perm string) bool {
	return hasPerm(k.Permissions, perm)
}
