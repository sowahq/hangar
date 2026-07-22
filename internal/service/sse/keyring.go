package sse

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/database"
)

const (
	defaultKeyID = "default"
	keyPrefix    = "ssekr:keys:"
	activeKey    = "ssekr:active"
)

var (
	ErrKeyNotFound  = errors.New("sse key not found")
	ErrNoActiveKey  = errors.New("sse keyring has no active key")
	ErrKeyExists    = errors.New("sse key already exists")
	ErrKeyInUse     = errors.New("sse key still referenced")
)

type Key struct {
	ID        string `json:"id"`
	Bytes     []byte `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
}

type KeyInfo struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
	Active    bool   `json:"active"`
}

var mu sync.RWMutex

func storeKey(k *Key) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	data, err := json.Marshal(k)
	if err != nil {
		return err
	}
	return db.Put([]byte(keyPrefix+k.ID), data)
}

func loadKey(id string) (*Key, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	data, err := db.Get([]byte(keyPrefix + id))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	var k Key
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

func setActiveID(id string) error {
	db := database.LocalStore()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	return db.Put([]byte(activeKey), []byte(id))
}

func activeID() (string, error) {
	db := database.LocalStore()
	if db == nil {
		return "", fmt.Errorf("database not initialized")
	}
	data, err := db.Get([]byte(activeKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return "", ErrNoActiveKey
		}
		return "", err
	}
	return string(data), nil
}

func Bootstrap(configMaster []byte) error {
	mu.Lock()
	defer mu.Unlock()

	if len(configMaster) == 0 {
		return nil
	}

	existing, err := loadKey(defaultKeyID)
	switch {
	case errors.Is(err, ErrKeyNotFound):
		k := &Key{ID: defaultKeyID, Bytes: append([]byte(nil), configMaster...), CreatedAt: time.Now().UnixMilli()}
		if err := storeKey(k); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if !bytes.Equal(existing.Bytes, configMaster) {
			existing.Bytes = append([]byte(nil), configMaster...)
			if err := storeKey(existing); err != nil {
				return err
			}
		}
	}

	if _, err := activeID(); errors.Is(err, ErrNoActiveKey) {
		return setActiveID(defaultKeyID)
	} else if err != nil {
		return err
	}

	return nil
}

func ActiveKey() (id string, bytes []byte, err error) {
	mu.RLock()
	defer mu.RUnlock()

	id, err = activeID()
	if err != nil {
		return "", nil, err
	}
	k, err := loadKey(id)
	if err != nil {
		return "", nil, err
	}
	return k.ID, append([]byte(nil), k.Bytes...), nil
}

func KeyBytes(id string) ([]byte, error) {
	mu.RLock()
	defer mu.RUnlock()

	if id == "" {
		id = defaultKeyID
	}
	k, err := loadKey(id)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), k.Bytes...), nil
}

func List() ([]KeyInfo, error) {
	mu.RLock()
	defer mu.RUnlock()

	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	cur, _ := activeID()

	iter, err := db.NewIteratorWithPrefix([]byte(keyPrefix))
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []KeyInfo
	for iter.First(); iter.Valid(); iter.Next() {
		var k Key
		if err := json.Unmarshal(iter.Value(), &k); err != nil {
			continue
		}
		out = append(out, KeyInfo{ID: k.ID, CreatedAt: k.CreatedAt, Active: k.ID == cur})
	}
	return out, nil
}

func Rotate() (string, error) {
	mu.Lock()
	defer mu.Unlock()

	id, err := newKeyID()
	if err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	k := &Key{ID: id, Bytes: buf, CreatedAt: time.Now().UnixMilli()}
	if err := storeKey(k); err != nil {
		return "", err
	}
	if err := setActiveID(id); err != nil {
		return "", err
	}
	return id, nil
}

func SetActive(id string) error {
	mu.Lock()
	defer mu.Unlock()

	if _, err := loadKey(id); err != nil {
		return err
	}
	return setActiveID(id)
}

func newKeyID() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("k-%x-%d", buf, time.Now().Unix()), nil
}
