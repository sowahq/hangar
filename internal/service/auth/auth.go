package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sowahq/hangar/internal/database"
	"github.com/cockroachdb/pebble"
	"golang.org/x/crypto/argon2"
)

var (
	ErrUnauthorized   = errors.New("unauthorized")
	ErrInvalidToken   = errors.New("invalid token")
	ErrTokenNotFound  = errors.New("token not found")
	ErrBucketMismatch = errors.New("token does not match bucket")
	ErrNoPermission   = errors.New("missing required permission")
)

const (
	PermRead   = "read"
	PermWrite  = "write"
	PermDelete = "delete"
	PermAdmin  = "admin"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

type Token struct {
	ID          string   `json:"id"`
	BucketName  string   `json:"bucket_name"`
	TokenHash   string   `json:"token_hash"`
	CreatedAt   int64    `json:"created_at"`
	Permissions []string `json:"permissions"`
}

func tokenKey(id string) []byte {
	return []byte("token:" + id)
}

func encodeArgon(salt, hash []byte) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
}

func decodeArgon(encoded string) (salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return nil, nil, ErrInvalidToken
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, ErrInvalidToken
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, ErrInvalidToken
	}
	return salt, hash, nil
}

func deriveID(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	enc := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(enc) < 12 {
		return enc
	}
	return enc[:12]
}

func CreateToken(bucket string, perms []string) (string, *Token, error) {
	if bucket == "" {
		return "", nil, fmt.Errorf("bucket required")
	}
	if len(perms) == 0 {
		return "", nil, fmt.Errorf("at least one permission required")
	}
	for _, p := range perms {
		switch p {
		case PermRead, PermWrite, PermDelete, PermAdmin:
		default:
			return "", nil, fmt.Errorf("invalid permission: %s", p)
		}
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", nil, fmt.Errorf("rand: %w", err)
	}
	secretStr := base64.RawURLEncoding.EncodeToString(secret)
	id := deriveID(secretStr)
	rawToken := id + "." + secretStr

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", nil, fmt.Errorf("rand salt: %w", err)
	}
	hash := argon2.IDKey([]byte(secretStr), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encoded := encodeArgon(salt, hash)

	tok := &Token{
		ID:          id,
		BucketName:  bucket,
		TokenHash:   encoded,
		CreatedAt:   nowUnixMilli(),
		Permissions: perms,
	}

	data, err := json.Marshal(tok)
	if err != nil {
		return "", nil, fmt.Errorf("marshal: %w", err)
	}
	db := database.LocalStore()
	if db == nil {
		return "", nil, fmt.Errorf("database not initialized")
	}
	if err := db.Put(tokenKey(id), data); err != nil {
		return "", nil, fmt.Errorf("store token: %w", err)
	}
	return rawToken, tok, nil
}

func VerifyToken(rawToken, bucket, requiredPerm string) (*Token, error) {
	if rawToken == "" {
		return nil, ErrInvalidToken
	}
	dot := strings.IndexByte(rawToken, '.')
	if dot <= 0 || dot == len(rawToken)-1 {
		return nil, ErrInvalidToken
	}
	id := rawToken[:dot]
	secretStr := rawToken[dot+1:]

	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	data, err := db.Get(tokenKey(id))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, ErrInvalidToken
	}

	salt, expected, err := decodeArgon(tok.TokenHash)
	if err != nil {
		return nil, ErrInvalidToken
	}
	got := argon2.IDKey([]byte(secretStr), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if subtle.ConstantTimeCompare(got, expected) != 1 {
		return nil, ErrUnauthorized
	}

	if tok.BucketName != bucket {
		return nil, ErrBucketMismatch
	}
	if requiredPerm != "" && !hasPerm(tok.Permissions, requiredPerm) {
		return nil, ErrNoPermission
	}
	return &tok, nil
}

func hasPerm(perms []string, want string) bool {
	for _, p := range perms {
		if p == want || p == PermAdmin {
			return true
		}
	}
	return false
}

func RevokeToken(id string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	exists, err := db.Exist(tokenKey(id))
	if err != nil {
		return err
	}
	if !exists {
		return ErrTokenNotFound
	}
	return db.Delete(tokenKey(id))
}

func ListTokens(bucket string) ([]Token, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	iter, err := db.NewIteratorWithPrefix([]byte("token:"))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []Token
	for iter.First(); iter.Valid(); iter.Next() {
		var t Token
		if err := json.Unmarshal(iter.Value(), &t); err != nil {
			continue
		}
		if bucket != "" && t.BucketName != bucket {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
