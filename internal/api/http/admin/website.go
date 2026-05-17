package admin

import (
	"encoding/json"
	"errors"

	"github.com/anhostfr/hangar/internal/api/http/response"
	bucketService "github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type websiteRequest struct {
	IndexDocument string `json:"index_document"`
	ErrorDocument string `json:"error_document,omitempty"`
}

func PutBucketWebsite(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	body := c.Body()
	if len(body) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Body required")
	}

	var req websiteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
	}

	cfg := &bucketService.WebsiteConfig{IndexDocument: req.IndexDocument, ErrorDocument: req.ErrorDocument}
	err := bucketService.PutWebsite(bucketName, cfg)
	recordAdmin(c, "website.set", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, cfg)
}

func GetBucketWebsite(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	cfg, err := bucketService.GetWebsite(bucketName)
	if err != nil {
		if errors.Is(err, bucketService.ErrWebsiteNotFound) {
			return response.Error(c, fiber.StatusNotFound, err.Error())
		}
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, cfg)
}

func DeleteBucketWebsite(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	err := bucketService.DeleteWebsite(bucketName)
	recordAdmin(c, "website.delete", "bucket", bucketName, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"deleted": true})
}
