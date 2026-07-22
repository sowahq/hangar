package s3

import (
	"net/http"
	"strings"
	"time"

	"github.com/sowahq/hangar/internal/service/object"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func normalizeETag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "W/")
	s = strings.Trim(s, `"`)
	return s
}

func etagMatches(headerVal, objETag string) bool {
	want := normalizeETag(objETag)
	for _, part := range strings.Split(headerVal, ",") {
		v := normalizeETag(part)
		if v == "*" || v == want {
			return true
		}
	}
	return false
}

func parseHTTPDate(s string) (time.Time, bool) {
	for _, layout := range []string{http.TimeFormat, time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func setNotModifiedHeaders(c *fiber.Ctx, m *storage.Metadatas) {
	c.Set("ETag", m.ETag)
	c.Set("Last-Modified", time.UnixMilli(m.CreatedAt).UTC().Format(http.TimeFormat))
}

func checkConditionalRead(c *fiber.Ctx, m *storage.Metadatas) int {
	lastMod := time.UnixMilli(m.CreatedAt).UTC().Truncate(time.Second)

	if ifMatch := c.Get("If-Match"); ifMatch != "" {
		if !etagMatches(ifMatch, m.ETag) {
			return fiber.StatusPreconditionFailed
		}
	} else if v := c.Get("If-Unmodified-Since"); v != "" {
		if t, ok := parseHTTPDate(v); ok && lastMod.After(t) {
			return fiber.StatusPreconditionFailed
		}
	}

	if ifNoneMatch := c.Get("If-None-Match"); ifNoneMatch != "" {
		if etagMatches(ifNoneMatch, m.ETag) {
			setNotModifiedHeaders(c, m)
			return fiber.StatusNotModified
		}
	} else if v := c.Get("If-Modified-Since"); v != "" {
		if t, ok := parseHTTPDate(v); ok && !lastMod.After(t) {
			setNotModifiedHeaders(c, m)
			return fiber.StatusNotModified
		}
	}

	return 0
}

func parseCopyConditions(c *fiber.Ctx) object.CopyConditions {
	cond := object.CopyConditions{
		IfMatch:     c.Get("x-amz-copy-source-if-match"),
		IfNoneMatch: c.Get("x-amz-copy-source-if-none-match"),
	}
	if v := c.Get("x-amz-copy-source-if-modified-since"); v != "" {
		if t, ok := parseHTTPDate(v); ok {
			cond.IfModifiedSince = t.UnixMilli()
		}
	}
	if v := c.Get("x-amz-copy-source-if-unmodified-since"); v != "" {
		if t, ok := parseHTTPDate(v); ok {
			cond.IfUnmodifiedSince = t.UnixMilli()
		}
	}
	return cond
}

func checkConditionalWrite(c *fiber.Ctx, bucketName, key string, lookup func(string, string) (*storage.Metadatas, error)) int {
	ifMatch := c.Get("If-Match")
	ifNoneMatch := c.Get("If-None-Match")

	if ifMatch == "" && ifNoneMatch == "" {
		return 0
	}

	m, err := lookup(bucketName, key)
	exists := err == nil && m != nil && !m.IsDeleteMarker

	if ifMatch != "" {
		if !exists {
			return fiber.StatusPreconditionFailed
		}
		if !etagMatches(ifMatch, m.ETag) {
			return fiber.StatusPreconditionFailed
		}
	}

	if ifNoneMatch != "" {
		if exists && etagMatches(ifNoneMatch, m.ETag) {
			return fiber.StatusPreconditionFailed
		}
	}

	return 0
}
