package handlers

import (
	"fmt"
	"io"

	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/anhostfr/hangar/internal/service/object"
)

func Download(c *fiber.Ctx) error {
	key := c.Params("*")
	if key == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Missing object key")
	}

	req := &object.GetObjectRequest{
		Key: key,
	}

	response, err := object.GetObject(req)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to get object: %s", key)
		return fiber.NewError(fiber.StatusNotFound, "Object not found")
	}

	c.Set("Content-Type", response.ContentType)
	c.Set("Content-Disposition", `attachment; filename="`+response.Filename+`"`)
	c.Set("Content-Length", fmt.Sprintf("%d", response.Size))

	// Stream with larger buffer for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	_, err = io.CopyBuffer(c.Response().BodyWriter(), response.Reader, buf)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to stream object: %s", key)
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to stream file")
	}

	log.Debug().Msgf("Object downloaded: %s", key)

	return nil
}
