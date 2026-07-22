package s3

import (
	"encoding/xml"
	"errors"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type DefaultRetentionXML struct {
	Mode  string `xml:"Mode,omitempty"`
	Days  int    `xml:"Days,omitempty"`
	Years int    `xml:"Years,omitempty"`
}

type ObjectLockRuleXML struct {
	DefaultRetention *DefaultRetentionXML `xml:"DefaultRetention,omitempty"`
}

type ObjectLockConfigurationXML struct {
	XMLName           xml.Name           `xml:"ObjectLockConfiguration"`
	Xmlns             string             `xml:"xmlns,attr,omitempty"`
	ObjectLockEnabled string             `xml:"ObjectLockEnabled,omitempty"`
	Rule              *ObjectLockRuleXML `xml:"Rule,omitempty"`
}

func toObjectLockXML(cfg *bucket.ObjectLockConfig) ObjectLockConfigurationXML {
	out := ObjectLockConfigurationXML{Xmlns: xmlNamespace}
	if cfg.Enabled {
		out.ObjectLockEnabled = "Enabled"
	}
	if cfg.DefaultRetention != nil {
		out.Rule = &ObjectLockRuleXML{
			DefaultRetention: &DefaultRetentionXML{
				Mode:  cfg.DefaultRetention.Mode,
				Days:  cfg.DefaultRetention.Days,
				Years: cfg.DefaultRetention.Years,
			},
		}
	}
	return out
}

func fromObjectLockXML(in *ObjectLockConfigurationXML) (*bucket.ObjectLockConfig, error) {
	if in.ObjectLockEnabled != "Enabled" {
		return nil, errors.New("ObjectLockEnabled must be Enabled")
	}

	cfg := &bucket.ObjectLockConfig{Enabled: true}
	if in.Rule != nil && in.Rule.DefaultRetention != nil {
		dr := in.Rule.DefaultRetention
		cfg.DefaultRetention = &bucket.DefaultRetention{
			Mode:  dr.Mode,
			Days:  dr.Days,
			Years: dr.Years,
		}
	}
	return cfg, nil
}

func handleGetBucketObjectLock(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	cfg, err := bucket.GetObjectLockConfig(name)
	if err != nil {
		if errors.Is(err, bucket.ErrObjectLockNotConfigured) {
			return writeError(c, fiber.StatusNotFound, "ObjectLockConfigurationNotFoundError", "Object Lock configuration does not exist for this bucket", "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return writeXML(c, fiber.StatusOK, toObjectLockXML(cfg))
}

func handlePutBucketObjectLock(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty object lock body", "/"+name)
	}

	var in ObjectLockConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	cfg, err := fromObjectLockXML(&in)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name)
	}

	if err := bucket.PutObjectLockConfig(name, cfg); err != nil {
		switch {
		case errors.Is(err, bucket.ErrObjectLockNeedsVersion):
			return writeError(c, fiber.StatusConflict, "InvalidBucketState", err.Error(), "/"+name)
		case errors.Is(err, bucket.ErrObjectLockInvalidMode),
			errors.Is(err, bucket.ErrObjectLockInvalidRetain):
			return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusOK)
}
