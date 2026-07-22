package bucket

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sowahq/hangar/internal/database"
)

func UpdateVersioning(name string, enabled bool) (*BucketInfo, error) {
	info, err := GetBucket(name)
	if err != nil {
		return nil, err
	}
	info.VersioningEnabled = enabled
	info.UpdatedAt = time.Now().UnixMilli()

	data, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	db := database.LocalStore()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	if err := db.Put([]byte(fmt.Sprintf("bucket:%s", name)), data); err != nil {
		return nil, err
	}
	return info, nil
}
