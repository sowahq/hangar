package admin

import (
	bucketService "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

// ListBuckets handles GET /admin/buckets
func ListBuckets(c *fiber.Ctx) error {
	response, err := bucketService.ListBuckets()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(response)
}

// CreateBucket handles PUT /admin/buckets/:bucket
func CreateBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	req := &bucketService.CreateBucketRequest{
		Name:   bucketName,
		Public: false, // Default to private
	}

	response, err := bucketService.CreateBucket(req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

// GetBucket handles GET /admin/buckets/:bucket
func GetBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	bucketInfo, err := bucketService.GetBucket(bucketName)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(bucketInfo)
}

// DeleteBucket handles DELETE /admin/buckets/:bucket
func DeleteBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	req := &bucketService.DeleteBucketRequest{
		Name:  bucketName,
		Force: false, // Default to non-force delete
	}

	if err := bucketService.DeleteBucket(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"bucket": bucketName,
		"status": "deleted",
	})
}