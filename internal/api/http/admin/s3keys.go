package admin

import (
	"encoding/json"
	"errors"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/gofiber/fiber/v2"
)

type createS3KeyRequest struct {
	Permissions []string `json:"permissions"`
	Buckets     []string `json:"buckets"`
}

type s3KeyResponse struct {
	AccessKeyID string   `json:"access_key_id"`
	SecretKey   string   `json:"secret_key,omitempty"`
	Permissions []string `json:"permissions"`
	Buckets     []string `json:"buckets"`
	CreatedAt   int64    `json:"created_at"`
}

func CreateS3Key(c *fiber.Ctx) error {
	var req createS3KeyRequest
	body := c.Body()
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
		}
	}
	if len(req.Permissions) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Permissions required")
	}

	k, err := auth.CreateS3Key(req.Permissions, req.Buckets)

	target := ""
	if k != nil {
		target = k.AccessKeyID
	}
	recordAdmin(c, "s3key.create", "s3key", target, err)

	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusBadRequest, err.Error(), err, "Failed to create s3 key")
	}

	c.Status(fiber.StatusCreated)
	return response.JSON(c, s3KeyResponse{
		AccessKeyID: k.AccessKeyID,
		SecretKey:   k.SecretKey,
		Permissions: k.Permissions,
		Buckets:     k.Buckets,
		CreatedAt:   k.CreatedAt,
	})
}

func ListS3Keys(c *fiber.Ctx) error {
	keys, err := auth.ListS3Keys()
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to list s3 keys", err, "list s3 keys")
	}
	out := make([]s3KeyResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, s3KeyResponse{
			AccessKeyID: k.AccessKeyID,
			Permissions: k.Permissions,
			Buckets:     k.Buckets,
			CreatedAt:   k.CreatedAt,
		})
	}
	return response.JSON(c, fiber.Map{"keys": out, "count": len(out)})
}

func DeleteS3Key(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "Missing access key id")
	}
	err := auth.RevokeS3Key(id)

	recordAdmin(c, "s3key.delete", "s3key", id, err)

	if err != nil {
		if errors.Is(err, auth.ErrS3KeyNotFound) {
			return response.Error(c, fiber.StatusNotFound, "S3 key not found")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to revoke s3 key", err, "revoke s3 key")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
