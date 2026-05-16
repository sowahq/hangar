package admin

import (
	"encoding/json"
	"errors"

	"github.com/anhostfr/hangar/internal/api/http/response"
	bucketService "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type objectLockRequest struct {
	Enabled          bool                              `json:"enabled"`
	DefaultRetention *bucketService.DefaultRetention   `json:"default_retention,omitempty"`
}

func PutBucketObjectLock(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var req objectLockRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	cfg := &bucketService.ObjectLockConfig{Enabled: req.Enabled, DefaultRetention: req.DefaultRetention}
	err := bucketService.PutObjectLockConfig(bucketName, cfg)
	recordAdmin(c, "objectlock.set", "bucket", bucketName, err)

	if err != nil {
		switch {
		case errors.Is(err, bucketService.ErrObjectLockNeedsVersion):
			return response.Error(c, fiber.StatusConflict, err.Error())
		case errors.Is(err, bucketService.ErrObjectLockInvalidMode),
			errors.Is(err, bucketService.ErrObjectLockInvalidRetain):
			return response.Error(c, fiber.StatusBadRequest, err.Error())
		}
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, cfg)
}

func GetBucketObjectLock(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetObjectLockConfig(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrObjectLockNotConfigured) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}
