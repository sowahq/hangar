package pathutil

import (
	"strings"
)

// SplitBucketKey splits a key into bucket and object parts
func SplitBucketKey(key string) (bucket, objectKey string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}

// JoinBucketKey creates a bucket-scoped key
func JoinBucketKey(bucket, objectKey string) string {
	if bucket == "" {
		return objectKey
	}
	return bucket + "/" + objectKey
}

