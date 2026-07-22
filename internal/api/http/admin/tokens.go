package admin

import (
	"encoding/json"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/service/auth"
	bucketService "github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type createTokenRequest struct {
	Permissions []string `json:"permissions"`
}

type createTokenResponse struct {
	Token       string   `json:"token"`
	ID          string   `json:"id"`
	Permissions []string `json:"permissions"`
	Bucket      string   `json:"bucket"`
}

func CreateToken(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	if _, err := bucketService.GetBucket(bucketName); err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	var req createTokenRequest
	body := c.Body()
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "Invalid JSON body")
		}
	}
	if len(req.Permissions) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "Permissions required")
	}

	raw, tok, err := auth.CreateToken(bucketName, req.Permissions)

	recordAdmin(c, "token.create", "bucket", bucketName, err)

	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusBadRequest, err.Error(), err, "Failed to create token")
	}

	c.Status(fiber.StatusCreated)
	return response.JSON(c, createTokenResponse{
		Token:       raw,
		ID:          tok.ID,
		Permissions: tok.Permissions,
		Bucket:      tok.BucketName,
	})
}

func ListTokens(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	if _, err := bucketService.GetBucket(bucketName); err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	tokens, err := auth.ListTokens(bucketName)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to list tokens", err, "list tokens")
	}

	type listed struct {
		ID          string   `json:"id"`
		Bucket      string   `json:"bucket"`
		Permissions []string `json:"permissions"`
		CreatedAt   int64    `json:"created_at"`
	}
	out := make([]listed, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, listed{ID: t.ID, Bucket: t.BucketName, Permissions: t.Permissions, CreatedAt: t.CreatedAt})
	}
	return response.JSON(c, fiber.Map{"tokens": out, "count": len(out)})
}

func DeleteToken(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "Missing token id")
	}
	err := auth.RevokeToken(id)

	recordAdmin(c, "token.delete", "token", id, err)

	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Token not found")
	}

	return c.SendStatus(fiber.StatusNoContent)
}
