package handlers

import "github.com/gofiber/fiber/v2"

// Healthz is an unauthenticated liveness probe returning a static OK payload.
func Healthz(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}
