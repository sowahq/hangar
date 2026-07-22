package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/config"
	"github.com/gofiber/fiber/v2"
)

// RequireAdminToken guards the admin API with a bearer token. When no token is
// configured the guard is a no-op, preserving the unauthenticated behaviour.
func RequireAdminToken() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := config.AdminToken()
		if token == "" {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return response.Error(c, fiber.StatusUnauthorized, "Missing or invalid Authorization header")
		}

		raw := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(raw), []byte(token)) != 1 {
			return response.Error(c, fiber.StatusUnauthorized, "Unauthorized")
		}

		return c.Next()
	}
}
