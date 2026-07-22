package admin

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sowahq/hangar/internal/api/http/response"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/lifecycle"
	"github.com/gofiber/fiber/v2"
)

func RunLifecycle(c *fiber.Ctx) error {
	stats, err := lifecycle.Run(context.Background())

	recordAdmin(c, "lifecycle.run", "system", "", err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, stats)
}

func PutBucketLifecycleAdmin(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var cfg bucketService.LifecycleConfiguration
	if err := json.Unmarshal(body, &cfg); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	err := bucketService.PutLifecycle(bucketName, &cfg)
	recordAdmin(c, "lifecycle.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, &cfg)
}

func GetBucketLifecycleAdmin(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetLifecycle(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrLifecycleNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}

func DeleteBucketLifecycleAdmin(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteLifecycle(bucketName)
	recordAdmin(c, "lifecycle.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
