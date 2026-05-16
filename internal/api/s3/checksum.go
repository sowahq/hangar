package s3

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

var checksumAlgorithms = []string{"crc32", "crc32c", "sha1", "sha256", "crc64nvme"}

func parseChecksum(c *fiber.Ctx) (algo, value string) {
	for _, a := range checksumAlgorithms {
		if v := strings.TrimSpace(c.Get("x-amz-checksum-" + a)); v != "" {
			return a, v
		}
	}

	if hint := strings.ToLower(strings.TrimSpace(c.Get("x-amz-sdk-checksum-algorithm"))); hint != "" {
		return hint, ""
	}

	return "", ""
}

func writeChecksumHeaders(c *fiber.Ctx, algo, value string) {
	if algo == "" || value == "" {
		return
	}

	c.Set("x-amz-checksum-"+algo, value)
	c.Set("x-amz-checksum-type", "FULL_OBJECT")
}
