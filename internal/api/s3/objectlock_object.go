package s3

import (
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

const (
	hdrObjectLockMode             = "x-amz-object-lock-mode"
	hdrObjectLockRetainUntilDate  = "x-amz-object-lock-retain-until-date"
	hdrObjectLockLegalHold        = "x-amz-object-lock-legal-hold"
	hdrBypassGovernanceRetention  = "x-amz-bypass-governance-retention"
)

type RetentionXML struct {
	XMLName         xml.Name `xml:"Retention"`
	Xmlns           string   `xml:"xmlns,attr,omitempty"`
	Mode            string   `xml:"Mode,omitempty"`
	RetainUntilDate string   `xml:"RetainUntilDate,omitempty"`
}

type LegalHoldXML struct {
	XMLName xml.Name `xml:"LegalHold"`
	Xmlns   string   `xml:"xmlns,attr,omitempty"`
	Status  string   `xml:"Status,omitempty"`
}

func parseObjectLockPutHeaders(c *fiber.Ctx) (*object.RetentionInput, bool, error) {
	mode := c.Get(hdrObjectLockMode)
	retain := c.Get(hdrObjectLockRetainUntilDate)
	hold := c.Get(hdrObjectLockLegalHold)

	var retention *object.RetentionInput
	if mode != "" || retain != "" {
		if mode == "" || retain == "" {
			return nil, false, errors.New("object-lock-mode and object-lock-retain-until-date must be set together")
		}
		if err := bucket.ValidateLockMode(mode); err != nil {
			return nil, false, err
		}
		t, err := time.Parse(time.RFC3339, retain)
		if err != nil {
			return nil, false, errors.New("invalid object-lock-retain-until-date: must be RFC3339")
		}
		retention = &object.RetentionInput{Mode: mode, RetainUntilMilli: t.UnixMilli()}
	}

	legalHold := false
	if hold != "" {
		switch strings.ToUpper(hold) {
		case "ON":
			legalHold = true
		case "OFF":
			legalHold = false
		default:
			return nil, false, errors.New("invalid x-amz-object-lock-legal-hold: must be ON or OFF")
		}
	}

	return retention, legalHold, nil
}

func bypassGovernance(c *fiber.Ctx) bool {
	v := strings.ToLower(c.Get(hdrBypassGovernanceRetention))
	if v != "true" {
		return false
	}
	k, ok := c.Locals("s3_key").(*auth.S3Key)
	if !ok || k == nil {
		return false
	}
	return k.HasPermission(auth.PermAdmin)
}

func echoObjectLockHeaders(c *fiber.Ctx, m *storage.Metadatas) {
	if m.ObjectLockMode != "" {
		c.Set(hdrObjectLockMode, m.ObjectLockMode)
		if m.ObjectLockRetainUntilMilli > 0 {
			c.Set(hdrObjectLockRetainUntilDate, time.UnixMilli(m.ObjectLockRetainUntilMilli).UTC().Format(time.RFC3339))
		}
	}
	if m.ObjectLockLegalHold {
		c.Set(hdrObjectLockLegalHold, "ON")
	}
}

func handleGetObjectRetention(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	versionID := c.Query("versionId")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	r, err := object.GetObjectRetention(name, key, versionID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
	}
	if r == nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchObjectLockConfiguration", "no retention on object", "/"+name+"/"+key)
	}

	out := RetentionXML{
		Xmlns:           xmlNamespace,
		Mode:            r.Mode,
		RetainUntilDate: time.UnixMilli(r.RetainUntilMilli).UTC().Format(time.RFC3339),
	}
	return writeXML(c, fiber.StatusOK, out)
}

func handlePutObjectRetention(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	versionID := c.Query("versionId")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	var in RetentionXML
	if err := xml.Unmarshal(c.Body(), &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name+"/"+key)
	}
	t, err := time.Parse(time.RFC3339, in.RetainUntilDate)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "invalid RetainUntilDate", "/"+name+"/"+key)
	}

	ret := &object.RetentionInput{Mode: in.Mode, RetainUntilMilli: t.UnixMilli()}

	err = object.PutObjectRetention(name, key, versionID, ret, bypassGovernance(c))
	if err != nil {
		switch {
		case errors.Is(err, object.ErrObjectLockBucketDisabled):
			return writeError(c, fiber.StatusBadRequest, "InvalidRequest", err.Error(), "/"+name+"/"+key)
		case errors.Is(err, object.ErrObjectLockShortenDenied),
			errors.Is(err, object.ErrObjectLockModeDowngrade):
			return writeError(c, fiber.StatusForbidden, "AccessDenied", err.Error(), "/"+name+"/"+key)
		case errors.Is(err, object.ErrObjectLockInvalidArgs):
			return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}

	return c.SendStatus(http.StatusOK)
}

func handleGetObjectLegalHold(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	versionID := c.Query("versionId")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	hold, err := object.GetObjectLegalHold(name, key, versionID)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
	}

	status := "OFF"
	if hold {
		status = "ON"
	}
	return writeXML(c, fiber.StatusOK, LegalHoldXML{Xmlns: xmlNamespace, Status: status})
}

func handlePutObjectLegalHold(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	versionID := c.Query("versionId")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	var in LegalHoldXML
	if err := xml.Unmarshal(c.Body(), &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name+"/"+key)
	}

	var hold bool
	switch strings.ToUpper(in.Status) {
	case "ON":
		hold = true
	case "OFF":
		hold = false
	default:
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "Status must be ON or OFF", "/"+name+"/"+key)
	}

	err := object.PutObjectLegalHold(name, key, versionID, hold)
	if err != nil {
		if errors.Is(err, object.ErrObjectLockBucketDisabled) {
			return writeError(c, fiber.StatusBadRequest, "InvalidRequest", err.Error(), "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}

	return c.SendStatus(http.StatusOK)
}
