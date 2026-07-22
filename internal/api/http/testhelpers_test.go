package http

import (
	"testing"

	"github.com/sowahq/hangar/internal/database"
)

func writeBucketDB(name string, data []byte) error {
	return database.LocalStore().Put([]byte("bucket:"+name), data)
}

func persistBucketRaw(t *testing.T, name string, data []byte) error {
	t.Helper()
	return writeBucketDB(name, data)
}
