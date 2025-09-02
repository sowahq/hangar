package path

import (
	"strings"
)

// SplitBucketKey splits a key into bucket and object parts
// Input: "bucket/path/to/object.txt" -> Output: ("bucket", "path/to/object.txt")
func SplitBucketKey(key string) (bucket, objectKey string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}

// JoinBucketKey creates a bucket-scoped key
// Input: ("bucket", "path/to/object.txt") -> Output: "bucket/path/to/object.txt"  
func JoinBucketKey(bucket, objectKey string) string {
	if bucket == "" {
		return objectKey
	}
	return bucket + "/" + objectKey
}

// ValidateBucketName checks if bucket name is valid
func ValidateBucketName(bucket string) bool {
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}
	
	// Basic validation - no special characters, dots, etc.
	for _, r := range bucket {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	
	// Cannot start or end with hyphen
	return bucket[0] != '-' && bucket[len(bucket)-1] != '-'
}