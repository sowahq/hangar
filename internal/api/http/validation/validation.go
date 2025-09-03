package validation

import (
	"strings"
	
	"github.com/gofiber/fiber/v2"
	"github.com/anhostfr/hangar/internal/api/http/response"
)

// ValidateKey validates object key from URL parameter
func ValidateKey(c *fiber.Ctx, paramName string) (string, error) {
	key := c.Params(paramName)
	if key == "" {
		return "", response.Error(c, fiber.StatusBadRequest, "Missing object key")
	}
	
	// Basic key validation
	if strings.Contains(key, "..") {
		return "", response.Error(c, fiber.StatusBadRequest, "Invalid key format")
	}
	
	return key, nil
}

// ValidateContentType validates request content type
func ValidateContentType(c *fiber.Ctx, allowedTypes ...string) error {
	contentType := c.Get("Content-Type")
	
	for _, allowed := range allowedTypes {
		if strings.Contains(contentType, allowed) {
			return nil
		}
	}
	
	return response.Error(c, fiber.StatusBadRequest, "Invalid content type")
}

// RejectMultipart rejects multipart/form-data requests
func RejectMultipart(c *fiber.Ctx) error {
	if c.Is("multipart/form-data") {
		return response.Error(c, fiber.StatusBadRequest, "Invalid request")
	}
	return nil
}