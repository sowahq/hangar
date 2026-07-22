package admin

import (
	"encoding/json"

	"github.com/sowahq/hangar/internal/api/http/response"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type publicRequest struct {
	Public bool `json:"public"`
}

func UpdatePublic(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	var req publicRequest
	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	info, err := bucketService.UpdatePublic(bucketName, req.Public)

	recordAdmin(c, "bucket.public", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return response.JSON(c, info)
}
