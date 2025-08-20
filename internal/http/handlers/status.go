package handlers

import "github.com/gofiber/fiber/v2"

func Status(c *fiber.Ctx) error {
	c.JSON(fiber.Map{
		"status": "OK",
	})

	return nil
}
