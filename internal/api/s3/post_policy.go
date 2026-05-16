package s3

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

type postPolicyDoc struct {
	Expiration string          `json:"expiration"`
	Conditions []json.RawMessage `json:"conditions"`
}

func handlePostPolicy(c *fiber.Ctx) error {
	bucketName := c.Params("bucket")

	form, err := c.MultipartForm()
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedPOSTRequest", err.Error(), "/"+bucketName)
	}

	get := func(k string) string {
		if v := form.Value[k]; len(v) > 0 {
			return v[0]
		}
		if v := form.Value[strings.ToLower(k)]; len(v) > 0 {
			return v[0]
		}
		return ""
	}

	policyB64 := get("policy")
	signature := get("x-amz-signature")
	credential := get("x-amz-credential")
	amzDate := get("x-amz-date")
	algorithm := get("x-amz-algorithm")
	keyTpl := get("key")

	if policyB64 == "" || signature == "" || credential == "" || amzDate == "" || keyTpl == "" {
		return writeError(c, fiber.StatusBadRequest, "MalformedPOSTRequest", "missing required form fields", "/"+bucketName)
	}
	if algorithm != "AWS4-HMAC-SHA256" {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "unsupported algorithm", "/"+bucketName)
	}

	credParts := strings.Split(credential, "/")
	if len(credParts) < 5 {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "malformed credential", "/"+bucketName)
	}
	accessKey, date, region, service := credParts[0], credParts[1], credParts[2], credParts[3]
	if service != "s3" {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "credential service must be s3", "/"+bucketName)
	}

	k, kErr := auth.GetS3Key(accessKey)
	if kErr != nil {
		return writeError(c, fiber.StatusForbidden, "InvalidAccessKeyId", "unknown access key", "/"+bucketName)
	}

	signingKey := DeriveSigningKey(k.SecretKey, date, region, service)
	expected := hex.EncodeToString(hmacSHA256(signingKey, []byte(policyB64)))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return writeError(c, fiber.StatusForbidden, "SignatureDoesNotMatch", "signature mismatch", "/"+bucketName)
	}

	rawPolicy, decErr := base64.StdEncoding.DecodeString(policyB64)
	if decErr != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedPolicy", decErr.Error(), "/"+bucketName)
	}

	var doc postPolicyDoc
	if err := json.Unmarshal(rawPolicy, &doc); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedPolicy", err.Error(), "/"+bucketName)
	}

	if doc.Expiration != "" {
		exp, expErr := time.Parse(time.RFC3339, doc.Expiration)
		if expErr != nil {
			return writeError(c, fiber.StatusBadRequest, "MalformedPolicy", "invalid expiration", "/"+bucketName)
		}
		if time.Now().After(exp) {
			return writeError(c, fiber.StatusForbidden, "AccessDenied", "policy expired", "/"+bucketName)
		}
	}

	fileHdr, fileErr := pickFormFile(form)
	if fileErr != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedPOSTRequest", fileErr.Error(), "/"+bucketName)
	}

	objectKey := strings.ReplaceAll(keyTpl, "${filename}", fileHdr.Filename)

	if err := validatePolicyConditions(doc.Conditions, bucketName, objectKey, fileHdr.Size, form.Value); err != nil {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", err.Error(), "/"+bucketName+"/"+objectKey)
	}

	if !k.HasPermission(auth.PermAdmin) && !k.HasPermission(auth.PermWrite) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName)
	}
	if !k.AllowsBucket(bucketName) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+bucketName)
	}

	if _, err := bucket.GetBucket(bucketName); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+bucketName)
	}

	src, openErr := fileHdr.Open()
	if openErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", openErr.Error(), "/"+bucketName)
	}
	defer src.Close()

	contentType := get("Content-Type")
	if contentType == "" {
		contentType = fileHdr.Header.Get("Content-Type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	res, putErr := object.PutObject(&object.PutObjectRequest{
		Bucket:        bucketName,
		Key:           objectKey,
		Body:          src,
		ContentLength: fileHdr.Size,
		ContentType:   contentType,
	})
	if putErr != nil {
		if errors.Is(putErr, object.ErrQuotaExceeded) {
			return writeError(c, fiber.StatusRequestEntityTooLarge, "EntityTooLarge", "Quota exceeded", "/"+bucketName+"/"+objectKey)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", putErr.Error(), "/"+bucketName+"/"+objectKey)
	}

	c.Set("ETag", res.ETag)
	if res.VersionID != "" {
		c.Set("x-amz-version-id", res.VersionID)
	}
	c.Set("Location", "/"+bucketName+"/"+objectKey)

	if status := get("success_action_status"); status == "201" {
		return c.SendStatus(fiber.StatusCreated)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func pickFormFile(form *multipart.Form) (*multipart.FileHeader, error) {
	if files, ok := form.File["file"]; ok && len(files) > 0 {
		return files[0], nil
	}
	for _, headers := range form.File {
		if len(headers) > 0 {
			return headers[0], nil
		}
	}
	return nil, fmt.Errorf("no file field in form")
}

func validatePolicyConditions(conditions []json.RawMessage, bucketName, objectKey string, size int64, formValues map[string][]string) error {
	for _, raw := range conditions {
		raw = []byte(strings.TrimSpace(string(raw)))
		if len(raw) == 0 {
			continue
		}
		if raw[0] == '[' {
			var arr []interface{}
			if err := json.Unmarshal(raw, &arr); err != nil {
				return fmt.Errorf("malformed condition: %w", err)
			}
			if len(arr) < 2 {
				return fmt.Errorf("malformed condition")
			}
			op, _ := arr[0].(string)
			switch op {
			case "content-length-range":
				if len(arr) != 3 {
					return fmt.Errorf("malformed content-length-range")
				}
				minV, _ := toInt64(arr[1])
				maxV, _ := toInt64(arr[2])
				if size < minV || size > maxV {
					return fmt.Errorf("content-length out of range")
				}
			case "eq", "starts-with":
				field, _ := arr[1].(string)
				val, _ := arr[2].(string)
				field = strings.TrimPrefix(strings.ToLower(field), "$")
				actual := lookupFormValue(formValues, field, bucketName, objectKey)
				if op == "eq" && actual != val {
					return fmt.Errorf("condition %s mismatch on %s", op, field)
				}
				if op == "starts-with" && !strings.HasPrefix(actual, val) {
					return fmt.Errorf("condition starts-with mismatch on %s", field)
				}
			}
		} else if raw[0] == '{' {
			var obj map[string]string
			if err := json.Unmarshal(raw, &obj); err != nil {
				return fmt.Errorf("malformed condition: %w", err)
			}
			for field, val := range obj {
				field = strings.ToLower(field)
				actual := lookupFormValue(formValues, field, bucketName, objectKey)
				if actual != val {
					return fmt.Errorf("condition mismatch on %s", field)
				}
			}
		}
	}
	return nil
}

func lookupFormValue(form map[string][]string, field, bucketName, objectKey string) string {
	switch field {
	case "bucket":
		return bucketName
	case "key":
		return objectKey
	}
	if v, ok := form[field]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func toInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int:
		return int64(t), true
	case int64:
		return t, true
	}
	return 0, false
}

