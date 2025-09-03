package handlers

import (
	"io"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/api/http/validation"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/pkg/ioutils"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func Download(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key, err := validation.ValidateKey(c, "*")
	if err != nil {
		return err
	}

	// Debug logging to track requests
	log.Debug().Msgf("Download request: bucket='%s', key='%s', path='%s'", bucketName, key, c.Path())

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

	c.Set("Content-Type", "application/octet-stream")
	c.Set("Content-Disposition", `attachment; filename="`+result.Filename+`"`)
	c.Set("Accept-Ranges", "none")

	log.Debug().Msgf("Starting stream for object: %s", key)

	ctx := c.Context()

	err = c.SendStream(ioutils.NewCancelableReader(ctx, result.Reader.(io.ReadCloser)), int(result.Size))
	if err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to stream file", err, "Failed to stream object: "+key)
	}

	log.Debug().Msgf("Object download completed: %s", key)

	return nil
}
