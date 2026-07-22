package s3

import (
	"io"
	"net/http"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/pkg/ioutils"
	"github.com/gofiber/fiber/v2"
)

func isAnonymous(c *fiber.Ctx) bool {
	v, _ := c.Locals("s3_anonymous").(bool)
	return v
}

func websiteServeIndex(c *fiber.Ctx, bucketName string) error {
	cfg, err := bucket.GetWebsite(bucketName)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "no website index", "/"+bucketName)
	}
	return websiteServeKey(c, bucketName, cfg.IndexDocument, fiber.StatusOK)
}

func websiteServeError(c *fiber.Ctx, bucketName string) error {
	cfg, err := bucket.GetWebsite(bucketName)
	if err != nil || cfg.ErrorDocument == "" {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+bucketName)
	}
	return websiteServeKey(c, bucketName, cfg.ErrorDocument, fiber.StatusNotFound)
}

func websiteServeKey(c *fiber.Ctx, bucketName, key string, status int) error {
	m, err := object.GetMetadata(bucketName, key)
	if err != nil || m == nil || m.IsDeleteMarker {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+bucketName+"/"+key)
	}

	c.Set("Content-Type", m.ContentType)
	c.Set("ETag", m.ETag)
	c.Set("Last-Modified", time.UnixMilli(m.CreatedAt).UTC().Format(http.TimeFormat))

	reader, rErr := object.NewChunkReaderAtWithSSE(m, 0, nil)
	if rErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", rErr.Error(), "/"+bucketName+"/"+key)
	}

	c.Status(status)
	ctx := c.Context()
	return c.SendStream(ioutils.NewCancelableReader(ctx, io.NopCloser(reader)), int(m.Size))
}

var _ = config.ChunkSize
var _ = storage.Metadatas{}
