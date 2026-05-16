package s3

import (
	"encoding/xml"
	"errors"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type lifecycleExpirationXML struct {
	Days int `xml:"Days,omitempty"`
}

type lifecycleAbortMultipartXML struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation,omitempty"`
}

type lifecycleFilterXML struct {
	Prefix string `xml:"Prefix,omitempty"`
}

type lifecycleRuleXML struct {
	ID                            string                      `xml:"ID,omitempty"`
	Status                        string                      `xml:"Status"`
	Prefix                        string                      `xml:"Prefix,omitempty"`
	Filter                        *lifecycleFilterXML         `xml:"Filter,omitempty"`
	Expiration                    *lifecycleExpirationXML     `xml:"Expiration,omitempty"`
	AbortIncompleteMultipartUpload *lifecycleAbortMultipartXML `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

type lifecycleConfigurationXML struct {
	XMLName xml.Name           `xml:"LifecycleConfiguration"`
	Xmlns   string             `xml:"xmlns,attr,omitempty"`
	Rules   []lifecycleRuleXML `xml:"Rule"`
}

func toLifecycleXML(cfg *bucket.LifecycleConfiguration) lifecycleConfigurationXML {
	out := lifecycleConfigurationXML{Xmlns: xmlNamespace}
	for _, r := range cfg.Rules {
		rx := lifecycleRuleXML{ID: r.ID, Prefix: r.Prefix, Status: "Disabled"}
		if r.Enabled {
			rx.Status = "Enabled"
		}
		if r.ExpirationDays > 0 {
			rx.Expiration = &lifecycleExpirationXML{Days: r.ExpirationDays}
		}
		if r.AbortMultipartAfterDays > 0 {
			rx.AbortIncompleteMultipartUpload = &lifecycleAbortMultipartXML{DaysAfterInitiation: r.AbortMultipartAfterDays}
		}
		out.Rules = append(out.Rules, rx)
	}
	return out
}

func fromLifecycleXML(in *lifecycleConfigurationXML) *bucket.LifecycleConfiguration {
	cfg := &bucket.LifecycleConfiguration{}
	for _, r := range in.Rules {
		prefix := r.Prefix
		if prefix == "" && r.Filter != nil {
			prefix = r.Filter.Prefix
		}
		rule := bucket.LifecycleRule{
			ID:      r.ID,
			Enabled: r.Status == "Enabled",
			Prefix:  prefix,
		}
		if r.Expiration != nil {
			rule.ExpirationDays = r.Expiration.Days
		}
		if r.AbortIncompleteMultipartUpload != nil {
			rule.AbortMultipartAfterDays = r.AbortIncompleteMultipartUpload.DaysAfterInitiation
		}
		cfg.Rules = append(cfg.Rules, rule)
	}
	return cfg
}

func handleGetBucketLifecycle(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	cfg, err := bucket.GetLifecycle(name)
	if err != nil {
		if errors.Is(err, bucket.ErrLifecycleNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchLifecycleConfiguration", "The lifecycle configuration does not exist", "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}
	return writeXML(c, fiber.StatusOK, toLifecycleXML(cfg))
}

func handlePutBucketLifecycle(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty lifecycle body", "/"+name)
	}

	var in lifecycleConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}
	if len(in.Rules) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "at least one Rule required", "/"+name)
	}

	for _, r := range in.Rules {
		if r.Status != "Enabled" && r.Status != "Disabled" {
			return writeError(c, fiber.StatusBadRequest, "MalformedXML", "Status must be Enabled or Disabled", "/"+name)
		}
		if r.Expiration == nil && r.AbortIncompleteMultipartUpload == nil {
			return writeError(c, fiber.StatusBadRequest, "MalformedXML", "Rule must have Expiration or AbortIncompleteMultipartUpload", "/"+name)
		}
	}

	if err := bucket.PutLifecycle(name, fromLifecycleXML(&in)); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucketLifecycle(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	if err := bucket.DeleteLifecycle(name); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
