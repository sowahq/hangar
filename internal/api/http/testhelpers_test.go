package http

import (
	"os"
	"testing"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/database"
)

func TestMain(m *testing.M) {
	config.SetAllowUnauthenticatedAdminForTest(true)
	os.Exit(m.Run())
}

func writeBucketDB(name string, data []byte) error {
	return database.LocalStore().Put([]byte("bucket:"+name), data)
}

func persistBucketRaw(t *testing.T, name string, data []byte) error {
	t.Helper()
	return writeBucketDB(name, data)
}
