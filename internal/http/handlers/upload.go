package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"

	"github.com/anhostfr/hangar/internal/http/response"
	"github.com/anhostfr/hangar/internal/http/validation"
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

	req := &object.PutObjectRequest{
		Bucket: bucketName,
		Key:    key,
		Body:   bodyStream,
	}

	result, err := object.PutObject(req)
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, err.Error(), err, "Failed to upload file: "+key)
	}

	log.Debug().Msgf("File uploaded: %s, Hash: %s, Size: %d", key, result.ObjectHash, result.Size)

	return response.JSON(c, result)
}
