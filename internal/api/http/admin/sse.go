package admin

import (
	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/service/sse"
	"github.com/gofiber/fiber/v2"
)

func ListSSEKeys(c *fiber.Ctx) error {
	keys, err := sse.List()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"keys": keys})
}

func RotateSSEKey(c *fiber.Ctx) error {
	id, err := sse.Rotate()

	recordAdmin(c, "sse.rotate", "sse_key", id, err)

	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{"active_key_id": id})
}

func ActivateSSEKey(c *fiber.Ctx) error {
	id := c.Params("id")
	err := sse.SetActive(id)

	recordAdmin(c, "sse.activate", "sse_key", id, err)

	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, fiber.Map{"active_key_id": id})
}
