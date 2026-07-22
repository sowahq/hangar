package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
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
