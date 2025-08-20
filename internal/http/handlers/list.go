package handlers

import (
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

// ListObjects handles GET /objects requests to list stored objects
func ListObjects(c *fiber.Ctx) error {
	prefix := c.Params("*")

	response, err := object.ListObjects(prefix)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(response)
}
