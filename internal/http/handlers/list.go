package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/anhostfr/hangar/internal/http/response"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
)

// ListObjects handles GET /buckets/:bucket/objects requests to list stored objects
func ListObjects(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	prefix := c.Params("*")

	// Validate bucket exists
	_, err := bucket.GetBucket(bucketName)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	result, err := object.ListObjectsInBucket(bucketName, prefix)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, err.Error(), err, "Failed to list objects")
	}

	return response.JSON(c, result)
}
