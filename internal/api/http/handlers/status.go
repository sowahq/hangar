package handlers

import (
	"github.com/gofiber/fiber/v2"
	
	"github.com/anhostfr/hangar/internal/api/http/response"
)

func Status(c *fiber.Ctx) error {
	return response.JSON(c, fiber.Map{
		"status": "OK",
	})
}
