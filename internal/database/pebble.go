package database

import (
	"fmt"

	"github.com/cockroachdb/pebble"
)

type nopLogger struct{}

func (nopLogger) Fatalf(format string, args ...interface{}) {}
func (nopLogger) Infof(format string, args ...interface{})  {}

type PebbleDB struct {
	db *pebble.DB
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
	return p.db.Set(key, value, pebble.Sync)
}

func (p *PebbleDB) Delete(key []byte) error {
	return p.db.Delete(key, pebble.Sync)
}

func (p *PebbleDB) Exist(key []byte) (bool, error) {
	_, closer, err := p.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	defer closer.Close()

	return true, nil
}

func (p *PebbleDB) DeleteBatch(keys [][]byte) error {
	if len(keys) == 0 {
		return nil
	}

	batch := p.db.NewBatch()
	defer batch.Close()

	for _, k := range keys {
		if err := batch.Delete(k, nil); err != nil {
			return err
		}
	}
	return batch.Commit(pebble.Sync)
}

func (p *PebbleDB) Close() error {
	return p.db.Close()
}

func (p *PebbleDB) NewIteratorWithPrefix(prefix []byte) (*pebble.Iterator, error) {
	return p.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: keyUpperBound(prefix),
	})
}

func keyUpperBound(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[: i+1]
		}
	}
	return nil
}
