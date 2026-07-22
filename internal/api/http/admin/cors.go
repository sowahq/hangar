package admin

import (
	"encoding/json"
	"errors"

	"github.com/sowahq/hangar/internal/api/http/response"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func PutBucketCORS(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var cfg bucketService.CORSConfiguration
	if err := json.Unmarshal(body, &cfg); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	err := bucketService.PutCORS(bucketName, &cfg)
	recordAdmin(c, "cors.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, &cfg)
}

func GetBucketCORS(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetCORS(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrCORSNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}

func DeleteBucketCORS(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteCORS(bucketName)
	recordAdmin(c, "cors.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
