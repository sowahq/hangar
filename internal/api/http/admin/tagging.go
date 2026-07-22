package admin

import (
	"encoding/json"
	"errors"

	"github.com/sowahq/hangar/internal/api/http/response"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func PutBucketTagging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var tags []bucketService.Tag
	if err := json.Unmarshal(body, &tags); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	err := bucketService.PutBucketTagging(bucketName, tags)
	recordAdmin(c, "tagging.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, tags)
}

func GetBucketTagging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	tags, err := bucketService.GetBucketTagging(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrTaggingNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, tags)
}

func DeleteBucketTagging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteBucketTagging(bucketName)
	recordAdmin(c, "tagging.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
