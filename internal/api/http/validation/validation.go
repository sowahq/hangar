package validation

import (
	"strings"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/gofiber/fiber/v2"
)

func ValidateKey(c *fiber.Ctx, paramName string) (string, error) {
	key := c.Params(paramName)
	if key == "" {
		return "", response.Error(c, fiber.StatusBadRequest, "Missing object key")
	}

	if strings.Contains(key, "..") {
		return "", response.Error(c, fiber.StatusBadRequest, "Invalid key format")
	}

	return key, nil
}

func ValidateContentType(c *fiber.Ctx, allowedTypes ...string) error {
	contentType := c.Get("Content-Type")

	for _, allowed := range allowedTypes {
		if strings.Contains(contentType, allowed) {
			return nil
		}
	}

	return response.Error(c, fiber.StatusBadRequest, "Invalid content type")
}

func RejectMultipart(c *fiber.Ctx) error {
	ct := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(ct, "multipart/form-data") {
		return response.Error(c, fiber.StatusBadRequest, "Invalid request")
	}
	return nil
}
