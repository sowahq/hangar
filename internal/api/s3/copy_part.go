package s3

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

func parseCopySourceRange(h string) (start, end int64, ok bool, err error) {
	if h == "" {
		return 0, 0, false, nil
	}
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, false, fmt.Errorf("invalid copy-source-range")
	}
	spec := strings.TrimPrefix(h, "bytes=")
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false, fmt.Errorf("invalid copy-source-range")
	}
	s, sErr := strconv.ParseInt(strings.TrimSpace(spec[:dash]), 10, 64)
	if sErr != nil {
		return 0, 0, false, fmt.Errorf("invalid copy-source-range start")
	}
	e, eErr := strconv.ParseInt(strings.TrimSpace(spec[dash+1:]), 10, 64)
	if eErr != nil {
		return 0, 0, false, fmt.Errorf("invalid copy-source-range end")
	}
	return s, e, true, nil
}

func handleUploadPartCopy(c *fiber.Ctx, dstBucket, dstKey, uploadID string, partNumber int, source string) error {
	srcBucket, srcKey, srcVersion, err := parseCopySource(source)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+dstBucket+"/"+dstKey)
	}

	if !keyAllowsBucket(c, srcBucket) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied to source bucket", "/"+srcBucket+"/"+srcKey)
	}

	srcSSE, srcErr := parseCopySourceSSERequest(c)
	if srcErr != nil {
		if ok, r := sseErrorResponse(c, srcErr, "/"+srcBucket+"/"+srcKey); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", srcErr.Error(), "/"+srcBucket+"/"+srcKey)
	}

	dstSSE, dstErr := parseSSERequest(c)
	if dstErr != nil {
		if ok, r := sseErrorResponse(c, dstErr, "/"+dstBucket+"/"+dstKey); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", dstErr.Error(), "/"+dstBucket+"/"+dstKey)
	}

	start, end, hasRange, rErr := parseCopySourceRange(c.Get("x-amz-copy-source-range"))
	if rErr != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", rErr.Error(), "/"+dstBucket+"/"+dstKey)
	}

	res, err := object.UploadPartCopy(&object.UploadPartCopyRequest{
		DstBucket:  dstBucket,
		DstKey:     dstKey,
		UploadID:   uploadID,
		PartNumber: partNumber,
		SrcBucket:  srcBucket,
		SrcKey:     srcKey,
		SrcVersion: srcVersion,
		SrcSSE:     srcSSE,
		DstSSE:     dstSSE,
		RangeStart: start,
		RangeEnd:   end,
		HasRange:   hasRange,
	})
	if err != nil {
		switch {
		case errors.Is(err, object.ErrCopySourceNotFound):
			return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+srcBucket+"/"+srcKey)
		case errors.Is(err, object.ErrMultipartNotFound):
			return writeError(c, fiber.StatusNotFound, "NoSuchUpload", err.Error(), "/"+dstBucket+"/"+dstKey)
		case errors.Is(err, object.ErrInvalidPartNumber):
			return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+dstBucket+"/"+dstKey)
		case errors.Is(err, object.ErrInsufficientStorage):
			return writeError(c, fiber.StatusInsufficientStorage, "InsufficientStorage", "Insufficient storage on node", "/"+dstBucket+"/"+dstKey)
		}
		if ok, r := sseErrorResponse(c, err, "/"+dstBucket+"/"+dstKey); ok {
			return r
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+dstBucket+"/"+dstKey)
	}

	if res.SrcVersionID != "" {
		c.Set("x-amz-copy-source-version-id", res.SrcVersionID)
	}
	if dstSSE != nil {
		echoSSEResponse(c, dstSSE.Algorithm, dstSSE.CustomerKeyMD5)
	}

	return writeXML(c, fiber.StatusOK, CopyPartResult{
		Xmlns:        xmlNamespace,
		ETag:         res.ETag,
		LastModified: time.UnixMilli(res.LastModified).UTC().Format(time.RFC3339),
	})
}
