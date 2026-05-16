package database

import (
	"os"
	"path/filepath"
)

var (
	localStore *PebbleDB
	dataDir    string
)

func Init(dir string, syncWrites bool) error {
	dataDir = dir
	path := filepath.Join(dataDir, "store")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	db, err := NewPebbleDBWithSync(path, syncWrites)
	if err != nil {
		return err
	}

	localStore = db
	return nil
}

func LocalStore() *PebbleDB {
	return localStore
}

func Close() error {
	if localStore == nil {
		return nil
	}
	err := localStore.Close()
	localStore = nil
	return err
}
