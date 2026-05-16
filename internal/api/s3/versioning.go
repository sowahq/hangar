package s3

import (
	"encoding/xml"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

func handleGetBucketVersioning(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	info, err := bucket.GetBucket(name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	out := VersioningConfigurationXML{Xmlns: xmlNamespace}
	if info.VersioningEnabled {
		out.Status = "Enabled"
	}

	return writeXML(c, fiber.StatusOK, out)
}

func handlePutBucketVersioning(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty versioning body", "/"+name)
	}

	var in VersioningConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	var enabled bool
	switch in.Status {
	case "Enabled":
		enabled = true
	case "Suspended":
		enabled = false
	default:
		return writeError(c, fiber.StatusBadRequest, "IllegalVersioningConfigurationException", "invalid Status: must be Enabled or Suspended", "/"+name)
	}

	if _, err := bucket.UpdateVersioning(name, enabled); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusOK)
}
