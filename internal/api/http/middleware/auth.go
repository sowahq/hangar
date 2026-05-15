package middleware

import (
	"strings"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func RequireAuth(perm string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		bucketName := c.Params("bucket")
		if bucketName != "" {
			info, err := bucket.GetBucket(bucketName)
			if err == nil && info.Public && c.Method() == fiber.MethodGet {
				return c.Next()
			}
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			return response.Error(c, fiber.StatusUnauthorized, "Missing or invalid Authorization header")
		}
		raw := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if raw == "" {
			return response.Error(c, fiber.StatusUnauthorized, "Empty bearer token")
		}

		tok, err := auth.VerifyToken(raw, bucketName, perm)
		if err != nil {
			return response.Error(c, fiber.StatusUnauthorized, "Unauthorized")
		}
		c.Locals("auth_token", tok)
		return c.Next()
	}
}
