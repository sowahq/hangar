package handlers

import (
	"github.com/gofiber/fiber/v2"
	
	"github.com/anhostfr/hangar/internal/http/response"
)

func Status(c *fiber.Ctx) error {
	return response.JSON(c, fiber.Map{
		"status": "OK",
	})
}
