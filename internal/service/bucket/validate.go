package bucket

import (
	"fmt"
	"net"
	"strings"
)

// BucketName validates bucket name according to AWS S3 rules
func BucketName(bucket string) error {
	if !validBucketName(bucket) {
		return fmt.Errorf("invalid bucket name: %s", bucket)
	}
	return nil
}

// validBucketName checks if bucket name is valid according to AWS S3 rules
func validBucketName(bucket string) bool {
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}

	if bucket[0] == '-' || bucket[0] == '.' || bucket[len(bucket)-1] == '-' || bucket[len(bucket)-1] == '.' {
		return false
	}

	// Check for valid characters and patterns
	prevChar := byte(0)
	for _, r := range bucket {
		char := byte(r)

		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '.') {
			return false
		}

		if char == '.' && prevChar == '.' {
			return false
		}

		if (char == '.' && prevChar == '-') || (char == '-' && prevChar == '.') {
			return false
		}

		prevChar = char
	}

	if net.ParseIP(bucket) != nil {
		return false
	}

	if strings.HasPrefix(bucket, "xn--") {
		return false
	}

	if strings.HasSuffix(bucket, "-s3alias") || strings.HasSuffix(bucket, "--ol-s3") {
		return false
	}

	return true
}