package handlers

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/api/http/validation"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
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

	if _, err = bucket.GetBucket(bucketName); err != nil {
		return response.Error(c, fiber.StatusNotFound, "Bucket not found: "+bucketName)
	}

	if c.Request().URI().QueryArgs().Has("versions") {
		res, err := object.ListVersions(bucketName, key)
		if err != nil {
			return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to list versions", err, "Failed to list versions: "+key)
		}
		return response.JSON(c, res)
	}
	versionID := c.Query("versionId")
	var metadata *storage.Metadatas
	if versionID != "" {
		m, err := object.GetVersionMetadata(bucketName, key, versionID)
		if err != nil {
			return response.ErrorWithLog(c, fiber.StatusNotFound, "Version not found", err, "Failed to get version: "+key)
		}
		metadata = m
	} else {
		m, err := object.GetMetadata(bucketName, key)
		if err != nil {
			return response.ErrorWithLog(c, fiber.StatusNotFound, "Object not found", err, "Failed to get metadata: "+key)
		}
		metadata = m
	}
	if metadata.IsDeleteMarker {
		c.Set("X-Delete-Marker", "true")
		return response.Error(c, fiber.StatusNotFound, "Object not found")
	}
	if metadata.VersionID != "" {
		c.Set("X-Version-Id", metadata.VersionID)
	}

	rangeHeader := c.Get("Range")
	filename := metadata.Key
	if i := strings.LastIndex(filename, "/"); i >= 0 {
		filename = filename[i+1:]
	}

	c.Set("Content-Type", "application/octet-stream")
	c.Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Set("Accept-Ranges", "bytes")

	if rangeHeader == "" {
		reader := object.NewChunkReaderAt(metadata, 0)
		ctx := c.Context()
		if err := c.SendStream(ioutils.NewCancelableReader(ctx, io.NopCloser(reader)), int(metadata.Size)); err != nil {
			return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to stream file", err, "Failed to stream object: "+key)
		}
		log.Debug().Msgf("Object download completed: %s", key)
		return nil
	}

	start, end, parseErr := parseRange(rangeHeader, metadata.Size)
	if parseErr != nil {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", metadata.Size))
		return response.Error(c, fiber.StatusRequestedRangeNotSatisfiable, parseErr.Error())
	}

	chunkSize := int64(config.ChunkSize())
	startIdx := int(start / chunkSize)
	offsetInFirst := start % chunkSize

	cr := object.NewChunkReaderAt(metadata, startIdx)
	if err := cr.SkipBytes(offsetInFirst); err != nil {
		_ = cr.Close()
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to seek", err, "seek failed")
	}

	length := end - start + 1
	limited := io.LimitReader(cr, length)

	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, metadata.Size))
	c.Status(fiber.StatusPartialContent)

	ctx := c.Context()
	if err := c.SendStream(ioutils.NewCancelableReader(ctx, readCloserWrap(limited, cr)), int(length)); err != nil {
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to stream file", err, "Failed to stream object: "+key)
	}

	return nil
}

func readCloserWrap(r io.Reader, c io.Closer) io.ReadCloser {
	return &rcWrap{r: r, c: c}
}

type rcWrap struct {
	r io.Reader
	c io.Closer
}

func (w *rcWrap) Read(p []byte) (int, error) { return w.r.Read(p) }
func (w *rcWrap) Close() error               { return w.c.Close() }

func parseRange(h string, size int64) (int64, int64, error) {
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range")
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, fmt.Errorf("multiple ranges not supported")
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, fmt.Errorf("invalid range")
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	if startStr == "" {
		if endStr == "" {
			return 0, 0, fmt.Errorf("invalid range")
		}
		suffix, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, fmt.Errorf("invalid range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, fmt.Errorf("invalid range")
	}
	var end int64
	if endStr == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range")
		}
	}
	if start > end || start >= size || end >= size {
		return 0, 0, fmt.Errorf("range not satisfiable")
	}
	return start, end, nil
}
