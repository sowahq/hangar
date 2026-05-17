package admin

import (
	"encoding/json"
	"errors"

	"github.com/anhostfr/hangar/internal/api/http/response"
	bucketService "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type loggingRequest struct {
	TargetBucket string `json:"target_bucket"`
	TargetPrefix string `json:"target_prefix,omitempty"`
}

func PutBucketLogging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var req loggingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	cfg := &bucketService.LoggingConfig{TargetBucket: req.TargetBucket, TargetPrefix: req.TargetPrefix}
	err := bucketService.PutLogging(bucketName, cfg)
	recordAdmin(c, "logging.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, cfg)
}

func GetBucketLogging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetLogging(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrLoggingNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}

func DeleteBucketLogging(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteLogging(bucketName)
	recordAdmin(c, "logging.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
