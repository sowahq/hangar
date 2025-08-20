package database

import (
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
)

// nopLogger is a no-op implementation of the Logger interface.
type nopLogger struct{}

func (nopLogger) Fatalf(format string, args ...interface{}) {}
func (nopLogger) Infof(format string, args ...interface{})  {}

type PebbleDB struct {
	db *pebble.DB
	mu sync.RWMutex
}

func NewPebbleDB(path string) (*PebbleDB, error) {
	db, err := pebble.Open(path, &pebble.Options{
		Logger: nopLogger{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}
	return &PebbleDB{db: db}, nil
}

func (p *PebbleDB) Get(key []byte) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	val, closer, err := p.db.Get(key)
	if err != nil {
		return nil, err
	}
	defer closer.Close()

	result := make([]byte, len(val))
	copy(result, val)
	return result, nil
}

func (p *PebbleDB) Put(key, value []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.Set(key, value, pebble.Sync)
}

func (p *PebbleDB) Delete(key []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.Delete(key, pebble.Sync)
}

func (p *PebbleDB) Exist(key []byte) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	_, closer, err := p.db.Get(key)
	if err != nil {
		return false, err
	}
	defer closer.Close()

	return true, nil
}

func (p *PebbleDB) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.Close()
}

func (p *PebbleDB) NewIteratorWithPrefix(prefix []byte) (*pebble.Iterator, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: nil,
	})
}
