package database

import (
	"os"
	"path/filepath"
)

var (
	localStore *PebbleDB
	dataDir    string
)

func Init(dir string) error {
	dataDir = dir
	path := filepath.Join(dataDir, "store")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	db, err := NewPebbleDB(path)
	if err != nil {
		return err
	}

	localStore = db
	return nil
}

func LocalStore() *PebbleDB {
	return localStore
}
