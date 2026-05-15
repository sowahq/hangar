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

func Delete(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key, err := validation.ValidateKey(c, "*")
	if err != nil {
		return err
	}

	if _, err := bucket.GetBucket(bucketName); err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	if err := object.DeleteObject(&object.DeleteObjectRequest{Bucket: bucketName, Key: key}); err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return response.Error(c, fiber.StatusNotFound, "Object not found")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to delete object", err, "Failed to delete object: "+key)
	}

	log.Debug().Msgf("Object deleted: bucket=%s key=%s", bucketName, key)
	return c.SendStatus(fiber.StatusNoContent)
}
