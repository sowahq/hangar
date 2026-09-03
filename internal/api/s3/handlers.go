package s3

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/sowahq/hangar/pkg/ioutils"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

const xmlContentType = "application/xml"

const (
	maxControlXMLBody = 8 << 20
	maxDeleteObjects  = 1000
	maxCompleteParts  = 10000
)

func writeXML(c *fiber.Ctx, status int, v any) error {
	c.Set(fiber.HeaderContentType, xmlContentType)
	c.Status(status)

	if _, err := c.Write([]byte(xml.Header)); err != nil {
		return err
	}

	return xml.NewEncoder(c).Encode(v)
}

func writeError(c *fiber.Ctx, status int, code, message, resource string) error {
	reqID, _ := c.Locals("s3_req_id").(string)
	if code == "InternalError" {
		log.Error().Str("req_id", reqID).Str("resource", resource).Str("detail", message).Msg("s3 internal error")
		message = "We encountered an internal error. Please try again."
	}
	return writeXML(c, status, ErrorXML{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestID: reqID,
	})
}

func formatS3Time(unixMilli int64) string {
	return time.UnixMilli(unixMilli).UTC().Format("2006-01-02T15:04:05.000Z")
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
	if c.Request().URI().QueryArgs().Has("cors") {
		return handlePutBucketCORS(c)
	}
	if c.Request().URI().QueryArgs().Has("lifecycle") {
		return handlePutBucketLifecycle(c)
	}
	if c.Request().URI().QueryArgs().Has("encryption") {
		return handlePutBucketEncryption(c)
	}
	if c.Request().URI().QueryArgs().Has("object-lock") {
		return handlePutBucketObjectLock(c)
	}
	if c.Request().URI().QueryArgs().Has("versioning") {
		return handlePutBucketVersioning(c)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handlePutBucketTagging(c)
	}
	if c.Request().URI().QueryArgs().Has("acl") {
		return handlePutBucketACL(c)
	}
	if c.Request().URI().QueryArgs().Has("policy") {
		return handlePutBucketPolicy(c)
	}
	if c.Request().URI().QueryArgs().Has("logging") {
		return handlePutBucketLogging(c)
	}
	if c.Request().URI().QueryArgs().Has("website") {
		return handlePutBucketWebsite(c)
	}
	if c.Request().URI().QueryArgs().Has("notification") {
		return handlePutBucketNotification(c)
	}
	if c.Request().URI().QueryArgs().Has("requestPayment") {
		return handlePutBucketRequestPayment(c)
	}

	name := c.Params("bucket")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	_, err := bucket.CreateBucket(&bucket.CreateBucketRequest{Name: name})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			c.Set("Location", "/"+name)
			return c.SendStatus(fiber.StatusOK)
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidBucketName", err.Error(), "/"+name)
	}

	c.Set("Location", "/"+name)
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucket(c *fiber.Ctx) error {
	if c.Request().URI().QueryArgs().Has("cors") {
		return handleDeleteBucketCORS(c)
	}
	if c.Request().URI().QueryArgs().Has("lifecycle") {
		return handleDeleteBucketLifecycle(c)
	}
	if c.Request().URI().QueryArgs().Has("encryption") {
		return handleDeleteBucketEncryption(c)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handleDeleteBucketTagging(c)
	}
	if c.Request().URI().QueryArgs().Has("policy") {
		return handleDeleteBucketPolicy(c)
	}
	if c.Request().URI().QueryArgs().Has("website") {
		return handleDeleteBucketWebsite(c)
	}

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

func handleObjectGet(c *fiber.Ctx) error {
	if c.Query("uploadId") != "" {
		return handleListParts(c)
	}
	if c.Request().URI().QueryArgs().Has("retention") {
		return handleGetObjectRetention(c)
	}
	if c.Request().URI().QueryArgs().Has("legal-hold") {
		return handleGetObjectLegalHold(c)
	}
	if c.Request().URI().QueryArgs().Has("attributes") {
		return handleGetObjectAttributes(c)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handleGetObjectTagging(c)
	}
	if c.Request().URI().QueryArgs().Has("acl") {
		return handleGetObjectACL(c)
	}
	return handleGetObject(c)
}

func handleObjectPut(c *fiber.Ctx) error {
	if c.Query("uploadId") != "" && c.Query("partNumber") != "" {
		return handleUploadPart(c)
	}
	if c.Request().URI().QueryArgs().Has("retention") {
		return handlePutObjectRetention(c)
	}
	if c.Request().URI().QueryArgs().Has("legal-hold") {
		return handlePutObjectLegalHold(c)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handlePutObjectTagging(c)
	}
	if c.Request().URI().QueryArgs().Has("acl") {
		return handlePutObjectACL(c)
	}
	return handlePutObject(c)
}

func handleObjectPost(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if c.Request().URI().QueryArgs().Has("uploads") {
		return handleInitiateMultipart(c, name, key)
	}

	if uploadID := c.Query("uploadId"); uploadID != "" {
		return handleCompleteMultipart(c, name, key, uploadID)
	}

	return writeError(c, fiber.StatusNotImplemented, "NotImplemented", "unsupported object POST", c.Path())
}

func handleObjectDelete(c *fiber.Ctx) error {
	if uploadID := c.Query("uploadId"); uploadID != "" {
		return handleAbortMultipart(c, uploadID)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handleDeleteObjectTagging(c)
	}
	return handleDeleteObject(c)
}

func handleInitiateMultipart(c *fiber.Ctx, bucketName, key string) error {
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName+"/"+key)
	}

	if _, err := bucket.GetBucket(bucketName); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+bucketName)
	}

	sseReq, sseErr := parseSSERequest(c)
	if sseErr != nil {
		if ok, r := sseErrorResponse(c, sseErr, "/"+bucketName+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", sseErr.Error(), "/"+bucketName+"/"+key)
	}

	sseReq = applyBucketDefaultSSE(bucketName, sseReq)

	res, err := object.InitiateMultipart(&object.InitiateMultipartRequest{
		Bucket:      bucketName,
		Key:         key,
		ContentType: string(c.Request().Header.ContentType()),
		SSE:         sseReq,
	})
	if err != nil {
		if ok, r := sseErrorResponse(c, err, "/"+bucketName+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName+"/"+key)
	}

	if sseReq != nil {
		echoSSEResponse(c, sseReq.Algorithm, sseReq.CustomerKeyMD5)
	}

	return writeXML(c, fiber.StatusOK, InitiateMultipartUploadResult{
		Xmlns:    xmlNamespace,
		Bucket:   bucketName,
		Key:      key,
		UploadID: res.UploadID,
	})
}

func handleUploadPart(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName+"/"+key)
	}

	uploadID := c.Query("uploadId")

	partNumber, err := strconv.Atoi(c.Query("partNumber"))
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "invalid partNumber", "/"+bucketName+"/"+key)
	}

	if src := c.Get("x-amz-copy-source"); src != "" {
		return handleUploadPartCopy(c, bucketName, key, uploadID, partNumber, src)
	}

	sseReq, sseErr := parseSSERequest(c)
	if sseErr != nil {
		if ok, r := sseErrorResponse(c, sseErr, "/"+bucketName+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", sseErr.Error(), "/"+bucketName+"/"+key)
	}

	partBody, _ := requestBody(c)

	partStreaming := false
	if ah, ok := c.Locals("s3_auth").(*AuthHeader); ok && ah != nil && ah.Streaming {
		partStreaming = true
	}
	partBody = maybeValidatingBody(c, partBody, partStreaming)

	checksumAlgo, checksumVal := parseChecksum(c)

	res, err := object.UploadPart(&object.UploadPartRequest{
		Bucket:            bucketName,
		Key:               key,
		UploadID:          uploadID,
		PartNumber:        partNumber,
		Body:              partBody,
		SSE:               sseReq,
		ChecksumAlgorithm: checksumAlgo,
		ChecksumValue:     checksumVal,
	})
	if err != nil {
		switch {
		case errors.Is(err, object.ErrInvalidPartNumber):
			return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrMultipartNotFound):
			return writeError(c, fiber.StatusNotFound, "NoSuchUpload", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrInsufficientStorage):
			return writeError(c, fiber.StatusInsufficientStorage, "InsufficientStorage", "Insufficient storage on node", "/"+bucketName+"/"+key)
		case errors.Is(err, ErrContentSHA256Mismatch):
			return writeError(c, fiber.StatusBadRequest, "XAmzContentSHA256Mismatch", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, ErrBadDigest):
			return writeError(c, fiber.StatusBadRequest, "BadDigest", err.Error(), "/"+bucketName+"/"+key)
		}
		if ok, r := sseErrorResponse(c, err, "/"+bucketName+"/"+key); ok {
			return r
		}

		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName+"/"+key)
	}

	c.Set("ETag", res.ETag)
	if sseReq != nil {
		echoSSEResponse(c, sseReq.Algorithm, sseReq.CustomerKeyMD5)
	}
	writeChecksumHeaders(c, res.ChecksumAlgorithm, res.ChecksumValue)
	return c.SendStatus(fiber.StatusOK)
}

func handleCompleteMultipart(c *fiber.Ctx, bucketName, key, uploadID string) error {
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName+"/"+key)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "missing CompleteMultipartUpload body", "/"+bucketName+"/"+key)
	}
	if len(body) > maxControlXMLBody {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "CompleteMultipartUpload body too large", "/"+bucketName+"/"+key)
	}

	var req CompleteMultipartUpload
	if err := xml.Unmarshal(body, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+bucketName+"/"+key)
	}
	if len(req.Parts) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "no parts specified", "/"+bucketName+"/"+key)
	}
	if len(req.Parts) > maxCompleteParts {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "too many parts specified", "/"+bucketName+"/"+key)
	}

	parts := make([]int, 0, len(req.Parts))
	expectedETags := make(map[int]string, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, p.PartNumber)
		if p.ETag != "" {
			expectedETags[p.PartNumber] = p.ETag
		}
	}

	res, err := object.CompleteMultipart(&object.CompleteMultipartRequest{
		Bucket:        bucketName,
		Key:           key,
		UploadID:      uploadID,
		Parts:         parts,
		ExpectedETags: expectedETags,
	})
	if err != nil {
		switch {
		case errors.Is(err, object.ErrMultipartNotFound):
			return writeError(c, fiber.StatusNotFound, "NoSuchUpload", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrNoPartsToComplete):
			return writeError(c, fiber.StatusBadRequest, "InvalidPart", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrPartMissing):
			return writeError(c, fiber.StatusBadRequest, "InvalidPart", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrPartETagMismatch):
			return writeError(c, fiber.StatusBadRequest, "InvalidPart", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrInvalidPartOrder):
			return writeError(c, fiber.StatusBadRequest, "InvalidPartOrder", err.Error(), "/"+bucketName+"/"+key)
		case errors.Is(err, object.ErrCompleteQuotaFail):
			return writeError(c, fiber.StatusRequestEntityTooLarge, "EntityTooLarge", "Quota exceeded", "/"+bucketName+"/"+key)
		}

		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName+"/"+key)
	}

	if res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	echoSSEResponse(c, res.SSEAlgorithm, res.SSECustomerMD5)

	return writeXML(c, fiber.StatusOK, CompleteMultipartUploadResult{
		Xmlns:    xmlNamespace,
		Location: "/" + bucketName + "/" + key,
		Bucket:   bucketName,
		Key:      key,
		ETag:     res.ETag,
	})
}

func handleAbortMultipart(c *fiber.Ctx, uploadID string) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermDelete) || !keyAllowsBucket(c, bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName+"/"+key)
	}

	if err := object.AbortMultipart(&object.AbortMultipartRequest{Bucket: bucketName, Key: key, UploadID: uploadID}); err != nil {
		if errors.Is(err, object.ErrMultipartNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchUpload", err.Error(), "/"+bucketName+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName+"/"+key)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func handleListMultipartUploads(c *fiber.Ctx, bucketName string) error {
	headers, err := storage.ScanBucketMultiparts(bucketName)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName)
	}

	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	keyMarker := c.Query("key-marker")
	uploadIDMarker := c.Query("upload-id-marker")

	maxUploads, _ := strconv.Atoi(c.Query("max-uploads"))
	if maxUploads <= 0 || maxUploads > 1000 {
		maxUploads = 1000
	}

	sort.Slice(headers, func(i, j int) bool {
		if headers[i].Key != headers[j].Key {
			return headers[i].Key < headers[j].Key
		}
		return headers[i].UploadID < headers[j].UploadID
	})

	out := ListMultipartUploadsResult{
		Xmlns:          xmlNamespace,
		Bucket:         bucketName,
		KeyMarker:      keyMarker,
		UploadIDMarker: uploadIDMarker,
		Prefix:         prefix,
		Delimiter:      delim,
		MaxUploads:     maxUploads,
	}

	commonPrefixes := map[string]bool{}
	past := keyMarker == "" && uploadIDMarker == ""

	for _, h := range headers {
		if !past {
			if h.Key < keyMarker || (h.Key == keyMarker && h.UploadID <= uploadIDMarker) {
				continue
			}
			past = true
		}

		if prefix != "" && !strings.HasPrefix(h.Key, prefix) {
			continue
		}

		if delim != "" {
			rest := strings.TrimPrefix(h.Key, prefix)
			if idx := strings.Index(rest, delim); idx >= 0 {
				commonPrefixes[prefix+rest[:idx+len(delim)]] = true
				continue
			}
		}

		if len(out.Uploads) >= maxUploads {
			out.IsTruncated = true
			out.NextKeyMarker = out.Uploads[len(out.Uploads)-1].Key
			out.NextUploadIDMarker = out.Uploads[len(out.Uploads)-1].UploadID
			break
		}

		out.Uploads = append(out.Uploads, MultipartUploadEntry{
			Key:       h.Key,
			UploadID:  h.UploadID,
			Initiated: formatS3Time(h.CreatedAt),
		})
	}

	cps := make([]string, 0, len(commonPrefixes))
	for p := range commonPrefixes {
		cps = append(cps, p)
	}
	sort.Strings(cps)
	for _, p := range cps {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: p})
	}

	return writeXML(c, fiber.StatusOK, out)
}

func handleListParts(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName+"/"+key)
	}

	uploadID := c.Query("uploadId")

	res, err := object.ListPartsService(bucketName, key, uploadID)
	if err != nil {
		if errors.Is(err, object.ErrMultipartNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchUpload", err.Error(), "/"+bucketName+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+bucketName+"/"+key)
	}

	out := ListPartsResult{
		Xmlns:    xmlNamespace,
		Bucket:   bucketName,
		Key:      key,
		UploadID: uploadID,
	}

	for _, p := range res.Parts {
		out.Parts = append(out.Parts, ListPart{
			PartNumber:   p.PartNumber,
			LastModified: formatS3Time(p.UploadedAt),
			ETag:         p.ETag,
			Size:         p.Size,
		})
	}

	return writeXML(c, fiber.StatusOK, out)
}

func requestBody(c *fiber.Ctx) (io.Reader, int64) {
	stream := c.Request().BodyStream()
	contentLength := int64(c.Request().Header.ContentLength())

	if ah, ok := c.Locals("s3_auth").(*AuthHeader); ok && ah != nil && ah.Streaming {
		decoded := int64(0)
		if v := c.Get("x-amz-decoded-content-length"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				decoded = n
			}
		}

		return newChunkedReader(stream, ah), decoded
	}

	return stream, contentLength
}

func handleBucketPost(c *fiber.Ctx) error {
	if c.Request().URI().QueryArgs().Has("delete") {
		return handleDeleteObjects(c)
	}

	if ct := string(c.Request().Header.ContentType()); strings.HasPrefix(ct, "multipart/form-data") {
		return handlePostPolicy(c)
	}

	return writeError(c, fiber.StatusNotImplemented, "NotImplemented", "unsupported bucket POST", c.Path())
}

func handleDeleteObjects(c *fiber.Ctx) error {
	name := c.Params("bucket")

	if !hasPerm(c, auth.PermDelete) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty delete body", "/"+name)
	}
	if len(body) > maxControlXMLBody {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "delete body too large", "/"+name)
	}

	var req DeleteRequest
	if err := xml.Unmarshal(body, &req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	if len(req.Objects) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "no objects to delete", "/"+name)
	}
	if len(req.Objects) > maxDeleteObjects {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "too many keys in a single delete request", "/"+name)
	}

	bypass := bypassGovernance(c)
	out := DeleteResult{Xmlns: xmlNamespace}
	for _, obj := range req.Objects {
		res, err := object.DeleteObject(&object.DeleteObjectRequest{
			Bucket:           name,
			Key:              obj.Key,
			VersionID:        obj.VersionID,
			BypassGovernance: bypass,
		})
		if err != nil && !errors.Is(err, object.ErrObjectNotFound) {
			code := "InternalError"
			if errors.Is(err, object.ErrObjectLockHeld) {
				code = "AccessDenied"
			}
			out.Errors = append(out.Errors, DeleteErrorObject{
				Key:     obj.Key,
				Code:    code,
				Message: err.Error(),
			})
			continue
		}

		if req.Quiet {
			continue
		}

		entry := DeletedObject{Key: obj.Key, VersionID: obj.VersionID}
		if res != nil {
			if res.IsDeleteMarker {
				entry.DeleteMarker = true
				entry.DeleteMarkerVersionID = res.VersionID
			} else if res.VersionID != "" {
				entry.VersionID = res.VersionID
			}
		}

		out.Deleted = append(out.Deleted, entry)
	}

	return writeXML(c, fiber.StatusOK, out)
}

func handleHeadBucket(c *fiber.Ctx) error {
	name := c.Params("bucket")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return c.SendStatus(fiber.StatusForbidden)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}

	region := config.S3Region()
	if region == "" {
		region = "us-east-1"
	}
	c.Set("x-amz-bucket-region", region)
	return c.SendStatus(fiber.StatusOK)
}

func handleListObjectsV2(c *fiber.Ctx) error {
	if c.Request().URI().QueryArgs().Has("cors") {
		return handleGetBucketCORS(c)
	}
	if c.Request().URI().QueryArgs().Has("lifecycle") {
		return handleGetBucketLifecycle(c)
	}
	if c.Request().URI().QueryArgs().Has("encryption") {
		return handleGetBucketEncryption(c)
	}
	if c.Request().URI().QueryArgs().Has("object-lock") {
		return handleGetBucketObjectLock(c)
	}
	if c.Request().URI().QueryArgs().Has("versions") {
		return handleListObjectVersions(c)
	}
	if c.Request().URI().QueryArgs().Has("versioning") {
		return handleGetBucketVersioning(c)
	}
	if c.Request().URI().QueryArgs().Has("tagging") {
		return handleGetBucketTagging(c)
	}
	if c.Request().URI().QueryArgs().Has("location") {
		return handleGetBucketLocation(c)
	}
	if c.Request().URI().QueryArgs().Has("acl") {
		return handleGetBucketACL(c)
	}
	if c.Request().URI().QueryArgs().Has("policy") {
		return handleGetBucketPolicy(c)
	}
	if c.Request().URI().QueryArgs().Has("logging") {
		return handleGetBucketLogging(c)
	}
	if c.Request().URI().QueryArgs().Has("website") {
		return handleGetBucketWebsite(c)
	}
	if c.Request().URI().QueryArgs().Has("notification") {
		return handleGetBucketNotification(c)
	}
	if c.Request().URI().QueryArgs().Has("requestPayment") {
		return handleGetBucketRequestPayment(c)
	}

	name := c.Params("bucket")

	if isAnonymous(c) {
		if info, err := bucket.GetBucket(name); err == nil && info.Public {
			if _, werr := bucket.GetWebsite(name); werr == nil {
				return websiteServeIndex(c, name)
			}
		}
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	if c.Request().URI().QueryArgs().Has("uploads") {
		return handleListMultipartUploads(c, name)
	}

	if c.Query("list-type") != "2" {
		return handleListObjectsV1(c, name)
	}

	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	contToken := c.Query("continuation-token")
	startAfter := c.Query("start-after")

	enc, encOK := listEncoding(c)
	if !encOK {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "invalid encoding-type", "/"+name)
	}

	maxKeys, ok := parseMaxKeys(c)
	if !ok {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "max-keys must be a non-negative integer", "/"+name)
	}

	out := ListBucketResultV2{
		Xmlns:             xmlNamespace,
		Name:              name,
		Prefix:            encodeListValue(enc, prefix),
		Delimiter:         encodeListValue(enc, delim),
		MaxKeys:           maxKeys,
		ContinuationToken: contToken,
		StartAfter:        encodeListValue(enc, startAfter),
		EncodingType:      enc,
	}

	if maxKeys == 0 {
		return writeXML(c, fiber.StatusOK, out)
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

	out.IsTruncated = res.IsTruncated
	out.NextContinuationToken = res.NextContinuationToken
	out.KeyCount = res.KeyCount

	for _, o := range res.Objects {
		out.Contents = append(out.Contents, Contents{
			Key:          encodeListValue(enc, o.Key),
			LastModified: formatS3Time(o.CreatedAt),
			ETag:         o.ETag,
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}

	for _, p := range res.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: encodeListValue(enc, p)})
	}

	return writeXML(c, fiber.StatusOK, out)
}

func parseMaxKeys(c *fiber.Ctx) (int, bool) {
	raw := c.Query("max-keys")
	if raw == "" {
		return 1000, true
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}

	if n > 1000 {
		n = 1000
	}

	return n, true
}

var responseOverrideHeaders = map[string]string{
	"response-content-type":        "Content-Type",
	"response-content-language":    "Content-Language",
	"response-expires":             "Expires",
	"response-cache-control":       "Cache-Control",
	"response-content-disposition": "Content-Disposition",
	"response-content-encoding":    "Content-Encoding",
}

func applyResponseOverrides(c *fiber.Ctx) {
	for param, header := range responseOverrideHeaders {
		if v := c.Query(param); v != "" {
			c.Set(header, v)
		}
	}
}

func setCacheControl(c *fiber.Ctx, public bool) {
	if public {
		c.Set("Cache-Control", "public, max-age=3600")
		return
	}
	c.Set("Cache-Control", "private, no-store")
}

func setObjectHeaders(c *fiber.Ctx, m *storage.Metadatas) {
	c.Set("Content-Type", m.ContentType)
	c.Set("ETag", m.ETag)
	c.Set("Last-Modified", time.UnixMilli(m.CreatedAt).UTC().Format(http.TimeFormat))
	c.Set("Accept-Ranges", "bytes")

	if m.VersionID != "" {
		c.Set("x-amz-version-id", m.VersionID)
	}

	writeSSEHeaders(c, m)
	writeChecksumHeaders(c, m.ChecksumAlgorithm, m.ChecksumValue)
	echoObjectLockHeaders(c, m)
	writeTaggingCount(c, m.Tags)
}

func handleHeadObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	anon := isAnonymous(c)
	if !anon && (!hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name)) {
		return c.SendStatus(fiber.StatusForbidden)
	}

	info, err := bucket.GetBucket(name)
	if err != nil {
		if anon {
			return c.SendStatus(fiber.StatusForbidden)
		}
		return c.SendStatus(fiber.StatusNotFound)
	}

	if anon && !info.Public {
		return c.SendStatus(fiber.StatusForbidden)
	}

	versionID := c.Query("versionId")

	var m *storage.Metadatas
	if versionID != "" {
		m, err = object.GetVersionMetadata(name, key, versionID)
	} else {
		m, err = object.GetMetadata(name, key)
	}
	if err != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}

	if m.IsDeleteMarker {
		c.Set("x-amz-delete-marker", "true")
		return c.SendStatus(fiber.StatusNotFound)
	}

	if st := checkConditionalRead(c, m); st != 0 {
		return c.SendStatus(st)
	}

	if m.SSEAlgorithm == object.SSEAlgoC {
		sseReq, sseErr := parseSSERequest(c)
		if sseErr != nil || sseReq == nil || sseReq.Algorithm != object.SSEAlgoC {
			return c.SendStatus(fiber.StatusBadRequest)
		}
		if sseReq.CustomerKeyMD5 != m.SSECustomerKeyMD5 {
			return c.SendStatus(fiber.StatusBadRequest)
		}
	}

	setObjectHeaders(c, m)
	setCacheControl(c, info.Public)
	applyResponseOverrides(c)
	c.Status(fiber.StatusOK)
	c.Response().Header.SetContentLength(int(m.Size))

	return nil
}

func handleGetObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	anon := isAnonymous(c)
	if !anon && (!hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name)) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	info, bErr := bucket.GetBucket(name)
	if bErr != nil {
		if anon {
			return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", bErr.Error(), "/"+name)
	}

	if anon && !info.Public {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	if anon && (key == "" || strings.HasSuffix(key, "/")) {
		cfg, cerr := bucket.GetWebsite(name)
		if cerr == nil {
			return websiteServeKey(c, name, key+cfg.IndexDocument, fiber.StatusOK)
		}
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
		if anon {
			return websiteServeError(c, name)
		}
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
	}

	if m.IsDeleteMarker {
		if anon {
			return websiteServeError(c, name)
		}
		c.Set("x-amz-delete-marker", "true")
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+name+"/"+key)
	}

	if st := checkConditionalRead(c, m); st != 0 {
		if st == fiber.StatusPreconditionFailed {
			return writeError(c, st, "PreconditionFailed", "At least one of the preconditions you specified did not hold", "/"+name+"/"+key)
		}
		return c.SendStatus(st)
	}

	sseReq, sseParseErr := parseSSERequest(c)
	if sseParseErr != nil {
		if ok, r := sseErrorResponse(c, sseParseErr, "/"+name+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", sseParseErr.Error(), "/"+name+"/"+key)
	}

	setObjectHeaders(c, m)
	setCacheControl(c, info.Public)
	applyResponseOverrides(c)

	if pn := c.Query("partNumber"); pn != "" {
		return serveObjectPart(c, name, key, m, sseReq, pn)
	}

	rangeHeader := c.Get("Range")

	if rangeHeader == "" || strings.Contains(rangeHeader, ",") {
		reader, rErr := object.NewChunkReaderAtWithSSE(m, 0, sseReq)
		if rErr != nil {
			if ok, r := sseErrorResponse(c, rErr, "/"+name+"/"+key); ok {
				return r
			}
			return writeError(c, fiber.StatusInternalServerError, "InternalError", rErr.Error(), "/"+name+"/"+key)
		}
		ctx := c.Context()
		return c.SendStream(ioutils.NewCancelableReader(ctx, io.NopCloser(reader)), int(m.Size))
	}

	start, end, parseErr := parseRange(rangeHeader, m.Size)
	if parseErr != nil {
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", m.Size))
		return writeError(c, fiber.StatusRequestedRangeNotSatisfiable, "InvalidRange", parseErr.Error(), "/"+name+"/"+key)
	}

	return serveByteRange(c, name, key, m, sseReq, start, end)
}

func serveObjectPart(c *fiber.Ctx, name, key string, m *storage.Metadatas, sseReq *object.SSERequest, pn string) error {
	partNum, err := strconv.Atoi(pn)
	if err != nil || partNum < 1 {
		return writeError(c, fiber.StatusBadRequest, "InvalidPartNumber", "invalid part number", "/"+name+"/"+key)
	}

	partCount := len(m.PartSizes)
	if partCount == 0 {
		if partNum != 1 {
			return writeError(c, fiber.StatusRequestedRangeNotSatisfiable, "InvalidPartNumber", "object has a single part", "/"+name+"/"+key)
		}
		c.Set("x-amz-mp-parts-count", "1")
		reader, rErr := object.NewChunkReaderAtWithSSE(m, 0, sseReq)
		if rErr != nil {
			if ok, r := sseErrorResponse(c, rErr, "/"+name+"/"+key); ok {
				return r
			}
			return writeError(c, fiber.StatusInternalServerError, "InternalError", rErr.Error(), "/"+name+"/"+key)
		}
		ctx := c.Context()
		return c.SendStream(ioutils.NewCancelableReader(ctx, io.NopCloser(reader)), int(m.Size))
	}

	if partNum > partCount {
		return writeError(c, fiber.StatusRequestedRangeNotSatisfiable, "InvalidPartNumber", "part number out of range", "/"+name+"/"+key)
	}

	var start int64
	for i := 0; i < partNum-1; i++ {
		start += m.PartSizes[i]
	}
	end := start + m.PartSizes[partNum-1] - 1

	c.Set("x-amz-mp-parts-count", strconv.Itoa(partCount))
	return serveByteRange(c, name, key, m, sseReq, start, end)
}

func serveByteRange(c *fiber.Ctx, name, key string, m *storage.Metadatas, sseReq *object.SSERequest, start, end int64) error {
	chunkSize := int64(config.ChunkSize())
	startIdx := int(start / chunkSize)
	offsetInFirst := start % chunkSize

	cr, crErr := object.NewChunkReaderAtWithSSE(m, startIdx, sseReq)
	if crErr != nil {
		if ok, r := sseErrorResponse(c, crErr, "/"+name+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", crErr.Error(), "/"+name+"/"+key)
	}
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

	if src := c.Get("x-amz-copy-source"); src != "" {
		return handleCopyObject(c, name, key, src)
	}

	if st := checkConditionalWrite(c, name, key, object.GetMetadata); st != 0 {
		return writeError(c, st, "PreconditionFailed", "At least one of the preconditions you specified did not hold", "/"+name+"/"+key)
	}

	sseReq, sseErr := parseSSERequest(c)
	if sseErr != nil {
		if ok, r := sseErrorResponse(c, sseErr, "/"+name+"/"+key); ok {
			return r
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", sseErr.Error(), "/"+name+"/"+key)
	}

	sseReq = applyBucketDefaultSSE(name, sseReq)

	retention, legalHold, lockErr := parseObjectLockPutHeaders(c)
	if lockErr != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", lockErr.Error(), "/"+name+"/"+key)
	}

	binfo, _ := bucket.GetBucket(name)
	if (retention != nil || legalHold) && (binfo == nil || !binfo.ObjectLockEnabled) {
		return writeError(c, fiber.StatusBadRequest, "InvalidRequest", "Object Lock not enabled on bucket", "/"+name+"/"+key)
	}
	if binfo != nil && binfo.ObjectLockEnabled {
		applied, _ := object.ApplyDefaultRetentionIfMissing(name, retention)
		retention = applied
	}

	bodyStream, contentLength := requestBody(c)

	streaming := false
	if ah, ok := c.Locals("s3_auth").(*AuthHeader); ok && ah != nil && ah.Streaming {
		streaming = true
	}
	bodyStream = maybeValidatingBody(c, bodyStream, streaming)

	checksumAlgo, checksumVal := parseChecksum(c)

	tags, tagErr := parseTaggingHeader(c.Get("x-amz-tagging"))
	if tagErr != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidTag", tagErr.Error(), "/"+name+"/"+key)
	}

	putReq := &object.PutObjectRequest{
		Bucket:              name,
		Key:                 key,
		Body:                bodyStream,
		ContentLength:       contentLength,
		ContentType:         string(c.Request().Header.ContentType()),
		SSE:                 sseReq,
		ChecksumAlgorithm:   checksumAlgo,
		ChecksumValue:       checksumVal,
		ObjectLockLegalHold: legalHold,
		Tags:                tags,
	}
	if retention != nil {
		putReq.ObjectLockMode = retention.Mode
		putReq.ObjectLockRetainUntilMilli = retention.RetainUntilMilli
	}

	res, err := object.PutObject(putReq)
	if err != nil {
		if errors.Is(err, object.ErrQuotaExceeded) {
			return writeError(c, fiber.StatusRequestEntityTooLarge, "EntityTooLarge", "Quota exceeded", "/"+name+"/"+key)
		}
		if errors.Is(err, object.ErrInsufficientStorage) {
			return writeError(c, fiber.StatusInsufficientStorage, "InsufficientStorage", "Insufficient storage on node", "/"+name+"/"+key)
		}
		if errors.Is(err, object.ErrLengthRequired) {
			return writeError(c, fiber.StatusLengthRequired, "MissingContentLength", "Content-Length required", "/"+name+"/"+key)
		}
		if errors.Is(err, object.ErrObjectLockHeld) {
			return writeError(c, fiber.StatusForbidden, "AccessDenied", err.Error(), "/"+name+"/"+key)
		}
		if errors.Is(err, ErrContentSHA256Mismatch) {
			return writeError(c, fiber.StatusBadRequest, "XAmzContentSHA256Mismatch", err.Error(), "/"+name+"/"+key)
		}
		if errors.Is(err, ErrBadDigest) {
			return writeError(c, fiber.StatusBadRequest, "BadDigest", err.Error(), "/"+name+"/"+key)
		}
		if ok, r := sseErrorResponse(c, err, "/"+name+"/"+key); ok {
			return r
		}

		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}

	c.Set("ETag", res.ETag)
	if res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	echoSSEResponse(c, res.SSEAlgorithm, res.SSECustomerMD5)
	writeChecksumHeaders(c, res.ChecksumAlgorithm, res.ChecksumValue)

	return c.SendStatus(fiber.StatusOK)
}

func handleCopyObject(c *fiber.Ctx, dstBucket, dstKey, source string) error {
	srcBucket, srcKey, srcVersion, err := parseCopySource(source)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+dstBucket+"/"+dstKey)
	}

	if !keyAllowsBucket(c, srcBucket) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied to source bucket", "/"+srcBucket+"/"+srcKey)
	}

	directive := strings.ToUpper(c.Get("x-amz-metadata-directive"))

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

	res, err := object.CopyObject(&object.CopyObjectRequest{
		SrcBucket:         srcBucket,
		SrcKey:            srcKey,
		SrcVersion:        srcVersion,
		DstBucket:         dstBucket,
		DstKey:            dstKey,
		MetadataDirective: directive,
		ContentType:       string(c.Request().Header.ContentType()),
		SrcSSE:            srcSSE,
		DstSSE:            dstSSE,
		Conditions:        parseCopyConditions(c),
	})
	if err != nil {
		if errors.Is(err, object.ErrCopyPreconditionFailed) {
			return writeError(c, fiber.StatusPreconditionFailed, "PreconditionFailed", "At least one of the preconditions you specified did not hold", "/"+srcBucket+"/"+srcKey)
		}
		if errors.Is(err, object.ErrCopySourceNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+srcBucket+"/"+srcKey)
		}
		if errors.Is(err, object.ErrQuotaExceeded) {
			return writeError(c, fiber.StatusRequestEntityTooLarge, "EntityTooLarge", "Quota exceeded", "/"+dstBucket+"/"+dstKey)
		}
		if errors.Is(err, object.ErrInsufficientStorage) {
			return writeError(c, fiber.StatusInsufficientStorage, "InsufficientStorage", "Insufficient storage on node", "/"+dstBucket+"/"+dstKey)
		}
		if ok, r := sseErrorResponse(c, err, "/"+dstBucket+"/"+dstKey); ok {
			return r
		}

		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+dstBucket+"/"+dstKey)
	}

	if res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	echoSSEResponse(c, res.SSEAlgorithm, res.SSECustomerMD5)
	writeChecksumHeaders(c, res.ChecksumAlgorithm, res.ChecksumValue)

	return writeXML(c, fiber.StatusOK, CopyObjectResult{
		ETag:         res.ETag,
		LastModified: formatS3Time(res.CreatedAt),
	})
}

func parseCopySource(src string) (bucket, key, version string, err error) {
	s := strings.TrimPrefix(src, "/")

	if idx := strings.IndexByte(s, '?'); idx >= 0 {
		q := s[idx+1:]
		s = s[:idx]

		for _, kv := range strings.Split(q, "&") {
			if rest, ok := strings.CutPrefix(kv, "versionId="); ok {
				version = rest
			}
		}
	}

	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash == len(s)-1 {
		return "", "", "", fmt.Errorf("invalid copy source: %q", src)
	}

	bucket = s[:slash]
	rawKey := s[slash+1:]

	decoded, decErr := url.PathUnescape(rawKey)
	if decErr != nil {
		return "", "", "", fmt.Errorf("invalid copy source key: %w", decErr)
	}

	key = decoded
	return
}

func handleDeleteObject(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermDelete) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	versionID := c.Query("versionId")

	res, err := object.DeleteObject(&object.DeleteObjectRequest{
		Bucket:           name,
		Key:              key,
		VersionID:        versionID,
		BypassGovernance: bypassGovernance(c),
	})
	if err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return c.SendStatus(fiber.StatusNoContent)
		}
		if errors.Is(err, object.ErrObjectLockHeld) {
			return writeError(c, fiber.StatusForbidden, "AccessDenied", err.Error(), "/"+name+"/"+key)
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

	if start >= size {
		return 0, 0, fmt.Errorf("range not satisfiable")
	}

	if end >= size {
		end = size - 1
	}

	if start > end {
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
