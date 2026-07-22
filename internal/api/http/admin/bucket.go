package admin

import (
	"encoding/json"

	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func ListBuckets(c *fiber.Ctx) error {
	response, err := bucketService.ListBuckets()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(response)
}

func CreateBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	req := &bucketService.CreateBucketRequest{
		Name: bucketName,
	}

	if body := c.Body(); len(body) > 0 {
		var opts publicRequest
		if err := json.Unmarshal(body, &opts); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid JSON body",
			})
		}
		req.Public = opts.Public
	}

	response, err := bucketService.CreateBucket(req)

	recordAdmin(c, "bucket.create", "bucket", bucketName, err)

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(fiber.StatusCreated).JSON(response)
}

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

func DeleteBucket(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	req := &bucketService.DeleteBucketRequest{
		Name:  bucketName,
		Force: false,
	}

	err := bucketService.DeleteBucket(req)

	recordAdmin(c, "bucket.delete", "bucket", bucketName, err)

	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"bucket": bucketName,
		"status": "deleted",
	})
}
