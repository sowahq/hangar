package admin

import (
	"context"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/service/lifecycle"
	"github.com/gofiber/fiber/v2"
)

func RunLifecycle(c *fiber.Ctx) error {
	stats, err := lifecycle.Run(context.Background())

	recordAdmin(c, "lifecycle.run", "system", "", err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, stats)
}
