package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/api/http/validation"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
)

func Upload(c *fiber.Ctx) error {
	if err := validation.RejectMultipart(c); err != nil {
		return err
	}

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

	bodyStream := c.Request().BodyStream()
	contentLength := int64(c.Request().Header.ContentLength())

	req := &object.PutObjectRequest{
		Bucket:        bucketName,
		Key:           key,
		Body:          bodyStream,
		ContentLength: contentLength,
	}

	result, err := object.PutObject(req)
	if err != nil {
		if errors.Is(err, object.ErrQuotaExceeded) {
			return response.Error(c, fiber.StatusRequestEntityTooLarge, "Quota exceeded")
		}
		if errors.Is(err, object.ErrLengthRequired) {
			return response.Error(c, fiber.StatusLengthRequired, "Content-Length required")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, err.Error(), err, "Failed to upload file: "+key)
	}

	log.Debug().Msgf("File uploaded: %s, Hash: %s, Size: %d", key, result.ObjectHash, result.Size)

	return response.JSON(c, result)
}
