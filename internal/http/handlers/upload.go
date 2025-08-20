package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/anhostfr/hangar/internal/service/object"
)

func Upload(c *fiber.Ctx) error {
	if c.Is("multipart/form-data") {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid request")
	}

	key := c.Params("*")
	bodyStream := c.Request().BodyStream()

	req := &object.PutObjectRequest{
		Key:  key,
		Body: bodyStream,
	}

	response, err := object.PutObject(req)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to upload file: %s", key)
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	log.Debug().Msgf("File uploaded: %s, Hash: %s, Size: %d", key, response.ObjectHash, response.Size)

	return c.JSON(response)
}
