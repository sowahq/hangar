package handlers

import (
	"fmt"
	"io"

	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/anhostfr/hangar/internal/http/response"
	"github.com/anhostfr/hangar/internal/http/validation"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
)

func Download(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key, err := validation.ValidateKey(c, "*")
	if err != nil {
		return err
	}

	// Validate bucket exists
	_, err = bucket.GetBucket(bucketName)
	if err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	req := &object.GetObjectRequest{
		Bucket: bucketName,
		Key:    key,
	}

	result, err := object.GetObject(req)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusNotFound, "Object not found", err, "Failed to get object: "+key)
	}

	c.Set("Content-Type", result.ContentType)
	c.Set("Content-Disposition", `attachment; filename="`+result.Filename+`"`)
	c.Set("Content-Length", fmt.Sprintf("%d", result.Size))

	// Stream with larger buffer for better performance
	buf := make([]byte, 64*1024) // 64KB buffer
	_, err = io.CopyBuffer(c.Response().BodyWriter(), result.Reader, buf)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to stream file", err, "Failed to stream object: "+key)
	}

	log.Debug().Msgf("Object downloaded: %s", key)

	return nil
}
