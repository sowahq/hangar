package admin

import (
	"encoding/json"

	"github.com/anhostfr/hangar/internal/api/http/response"
	bucketService "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type versioningRequest struct {
	Enabled bool `json:"enabled"`
}

func UpdateVersioning(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	var req versioningRequest
	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	info, err := bucketService.UpdateVersioning(bucketName, req.Enabled)

	recordAdmin(c, "bucket.versioning", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.JSON(c, info)
}
