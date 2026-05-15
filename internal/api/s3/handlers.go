package s3

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/anhostfr/hangar/pkg/ioutils"
	"github.com/gofiber/fiber/v2"
)

const xmlContentType = "application/xml"

func writeXML(c *fiber.Ctx, status int, v any) error {
	c.Set(fiber.HeaderContentType, xmlContentType)
	c.Status(status)
	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}
	return xml.NewEncoder(c).Encode(v)
}

func writeError(c *fiber.Ctx, status int, code, message, resource string) error {
	return writeXML(c, status, ErrorXML{
		Code:     code,
		Message:  message,
		Resource: resource,
	})
}

func formatS3Time(unixMilli int64) string {
	return time.UnixMilli(unixMilli).UTC().Format(time.RFC3339)
}

func currentKey(c *fiber.Ctx) string {
	return c.Locals("s3_key").(*auth.S3Key).AccessKeyID
}

func hasPerm(c *fiber.Ctx, perm string) bool {
	k, ok := c.Locals("s3_key").(*auth.S3Key)
	if !ok || k == nil {
		return false
	}
	if k.HasPermission(auth.PermAdmin) {
		return true
	}
	return k.HasPermission(perm)
}

func keyAllowsBucket(c *fiber.Ctx, name string) bool {
	k, ok := c.Locals("s3_key").(*auth.S3Key)
	if !ok || k == nil {
		return false
	}
	return k.AllowsBucket(name)
}

func handleListBuckets(c *fiber.Ctx) error {
	if !hasPerm(c, auth.PermRead) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/")
	}
	res, err := bucket.ListBuckets()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/")
	}
	out := ListAllMyBucketsResult{
		Xmlns: xmlNamespace,
		Owner: Owner{ID: currentKey(c), DisplayName: currentKey(c)},
	}
	for _, b := range res.Buckets {
		if !keyAllowsBucket(c, b.Name) {
			continue
		}
		out.Buckets = append(out.Buckets, BucketEntry{
			Name:         b.Name,
			CreationDate: formatS3Time(b.CreatedAt),
		})
	}
	return writeXML(c, fiber.StatusOK, out)
}

func handleCreateBucket(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	_, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: name})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return writeError(c, fiber.StatusConflict, "BucketAlreadyOwnedByYou", err.Error(), "/"+name)
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidBucketName", err.Error(), "/"+name)
	}
	c.Set("Location", "/"+name)
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucket(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermDelete) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if err := bucket.DeleteBucket(&bucket.DeleteBucketRequest{Name: name}); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			return writeError(c, fiber.StatusNotFound, "NoSuchBucket", msg, "/"+name)
		}
		if strings.Contains(msg, "not empty") {
			return writeError(c, fiber.StatusConflict, "BucketNotEmpty", msg, "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", msg, "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func handleHeadBucket(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleListObjectsV2(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	contToken := c.Query("continuation-token")
	startAfter := c.Query("start-after")
	maxKeys, _ := strconv.Atoi(c.Query("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	res, err := object.ListObjectsV2(&object.ListObjectsV2Request{
		Bucket:            name,
		Prefix:            prefix,
		Delimiter:         delim,
		ContinuationToken: contToken,
		StartAfter:        startAfter,
		MaxKeys:           maxKeys,
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	out := ListBucketResultV2{
		Xmlns:                 xmlNamespace,
		Name:                  name,
		Prefix:                prefix,
		Delimiter:             delim,
		MaxKeys:               maxKeys,
		IsTruncated:           res.IsTruncated,
		ContinuationToken:     contToken,
		NextContinuationToken: res.NextContinuationToken,
		StartAfter:            startAfter,
		KeyCount:              res.KeyCount,
	}
	for _, o := range res.Objects {
		out.Contents = append(out.Contents, Contents{
			Key:          o.Key,
			LastModified: formatS3Time(o.CreatedAt),
			ETag:         o.ETag,
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}
	for _, p := range res.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: p})
	}
	return writeXML(c, fiber.StatusOK, out)
}

func setObjectHeaders(c *fiber.Ctx, m *storage.Metadatas) {
	c.Set("Content-Type", m.ContentType)
	c.Set("ETag", m.ETag)
	c.Set("Last-Modified", time.UnixMilli(m.CreatedAt).UTC().Format(http.TimeFormat))
	c.Set("Accept-Ranges", "bytes")
	if m.VersionID != "" {
		c.Set("x-amz-version-id", m.VersionID)
	}
}

func handleHeadObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	m, err := object.GetMetadata(name, key)
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	if m.IsDeleteMarker {
		c.Set("x-amz-delete-marker", "true")
		return c.SendStatus(fiber.StatusNotFound)
	}
	setObjectHeaders(c, m)
	c.Status(fiber.StatusOK)
	c.Response().Header.SetContentLength(int(m.Size))
	return nil
}

func handleGetObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	versionID := c.Query("versionId")
	var m *storage.Metadatas
	var err error
	if versionID != "" {
		m, err = object.GetVersionMetadata(name, key, versionID)
	} else {
		m, err = object.GetMetadata(name, key)
	}
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
	}
	if m.IsDeleteMarker {
		c.Set("x-amz-delete-marker", "true")
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+name+"/"+key)
	}

	setObjectHeaders(c, m)
	rangeHeader := c.Get("Range")

	if rangeHeader == "" {
		reader := object.NewChunkReaderAt(m, 0)
		ctx := c.Context()
		return c.SendStream(ioutils.NewCancelableReader(ctx, io.NopCloser(reader)), int(m.Size))
	}

	start, end, parseErr := parseRange(rangeHeader, m.Size)
	if parseErr != nil {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", m.Size))
		return writeError(c, fiber.StatusRequestedRangeNotSatisfiable, "InvalidRange", parseErr.Error(), "/"+name+"/"+key)
	}
	chunkSize := int64(config.ChunkSize())
	startIdx := int(start / chunkSize)
	offsetInFirst := start % chunkSize
	cr := object.NewChunkReaderAt(m, startIdx)
	if err := cr.SkipBytes(offsetInFirst); err != nil {
		_ = cr.Close()
		return writeError(c, fiber.StatusInternalServerError, "InternalError", "seek failed", "/"+name+"/"+key)
	}
	length := end - start + 1
	limited := io.LimitReader(cr, length)
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, m.Size))
	c.Status(fiber.StatusPartialContent)
	ctx := c.Context()
	return c.SendStream(ioutils.NewCancelableReader(ctx, readCloserWrap(limited, cr)), int(length))
}

func handlePutObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	bodyStream := c.Request().BodyStream()
	contentLength := int64(c.Request().Header.ContentLength())

	res, err := object.PutObject(&object.PutObjectRequest{
		Bucket:        name,
		Key:           key,
		Body:          bodyStream,
		ContentLength: contentLength,
		ContentType:   string(c.Request().Header.ContentType()),
	})
	if err != nil {
		if errors.Is(err, object.ErrQuotaExceeded) {
			return writeError(c, fiber.StatusRequestEntityTooLarge, "EntityTooLarge", "Quota exceeded", "/"+name+"/"+key)
		}
		if errors.Is(err, object.ErrLengthRequired) {
			return writeError(c, fiber.StatusLengthRequired, "MissingContentLength", "Content-Length required", "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}
	c.Set("ETag", res.ETag)
	if res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermDelete) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	versionID := c.Query("versionId")
	res, err := object.DeleteObject(&object.DeleteObjectRequest{
		Bucket:    name,
		Key:       key,
		VersionID: versionID,
	})
	if err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return c.SendStatus(fiber.StatusNoContent)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}
	if res != nil && res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	if res != nil && res.IsDeleteMarker {
		c.Set("x-amz-delete-marker", "true")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

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

func readCloserWrap(r io.Reader, c io.Closer) io.ReadCloser {
	return &rcWrap{r: r, c: c}
}

type rcWrap struct {
	r io.Reader
	c io.Closer
}

func (w *rcWrap) Read(p []byte) (int, error) { return w.r.Read(p) }
func (w *rcWrap) Close() error               { return w.c.Close() }
