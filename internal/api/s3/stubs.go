package s3

import (
	"encoding/xml"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type GranteeXML struct {
	XMLName     xml.Name `xml:"Grantee"`
	XmlnsXSI    string   `xml:"xmlns:xsi,attr,omitempty"`
	Type        string   `xml:"xsi:type,attr"`
	ID          string   `xml:"ID,omitempty"`
	DisplayName string   `xml:"DisplayName,omitempty"`
}

type GrantXML struct {
	Grantee    GranteeXML `xml:"Grantee"`
	Permission string     `xml:"Permission"`
}

type AccessControlPolicyXML struct {
	XMLName           xml.Name   `xml:"AccessControlPolicy"`
	Xmlns             string     `xml:"xmlns,attr,omitempty"`
	Owner             Owner      `xml:"Owner"`
	AccessControlList []GrantXML `xml:"AccessControlList>Grant"`
}

type LoggingEnabledXML struct {
	TargetBucket string `xml:"TargetBucket"`
	TargetPrefix string `xml:"TargetPrefix,omitempty"`
}

type BucketLoggingStatusXML struct {
	XMLName        xml.Name           `xml:"BucketLoggingStatus"`
	Xmlns          string             `xml:"xmlns,attr,omitempty"`
	LoggingEnabled *LoggingEnabledXML `xml:"LoggingEnabled,omitempty"`
}

type IndexDocumentXML struct {
	Suffix string `xml:"Suffix"`
}

type ErrorDocumentXML struct {
	Key string `xml:"Key"`
}

type WebsiteConfigurationXML struct {
	XMLName       xml.Name          `xml:"WebsiteConfiguration"`
	Xmlns         string            `xml:"xmlns,attr,omitempty"`
	IndexDocument *IndexDocumentXML `xml:"IndexDocument,omitempty"`
	ErrorDocument *ErrorDocumentXML `xml:"ErrorDocument,omitempty"`
}

type NotificationConfigurationXML struct {
	XMLName xml.Name `xml:"NotificationConfiguration"`
	Xmlns   string   `xml:"xmlns,attr,omitempty"`
}

type RequestPaymentConfigurationXML struct {
	XMLName xml.Name `xml:"RequestPaymentConfiguration"`
	Xmlns   string   `xml:"xmlns,attr,omitempty"`
	Payer   string   `xml:"Payer"`
}

func defaultACL(c *fiber.Ctx) AccessControlPolicyXML {
	owner := Owner{ID: currentKey(c), DisplayName: currentKey(c)}
	return AccessControlPolicyXML{
		Xmlns: xmlNamespace,
		Owner: owner,
		AccessControlList: []GrantXML{{
			Grantee: GranteeXML{
				XmlnsXSI:    "http://www.w3.org/2001/XMLSchema-instance",
				Type:        "CanonicalUser",
				ID:          owner.ID,
				DisplayName: owner.DisplayName,
			},
			Permission: "FULL_CONTROL",
		}},
	}
}

func handleGetBucketACL(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return writeXML(c, fiber.StatusOK, defaultACL(c))
}

func handlePutBucketACL(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleGetObjectACL(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return writeXML(c, fiber.StatusOK, defaultACL(c))
}

func handlePutObjectACL(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleGetBucketPolicy(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return writeError(c, fiber.StatusNotFound, "NoSuchBucketPolicy", "The bucket policy does not exist", "/"+name)
}

func handlePutBucketPolicy(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func handleDeleteBucketPolicy(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func handleGetBucketLogging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	out := BucketLoggingStatusXML{Xmlns: xmlNamespace}
	cfg, err := bucket.GetLogging(name)
	if err == nil && cfg != nil {
		out.LoggingEnabled = &LoggingEnabledXML{
			TargetBucket: cfg.TargetBucket,
			TargetPrefix: cfg.TargetPrefix,
		}
	}
	return writeXML(c, fiber.StatusOK, out)
}

func handlePutBucketLogging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty logging body", "/"+name)
	}

	var in BucketLoggingStatusXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	if in.LoggingEnabled == nil {
		if err := bucket.DeleteLogging(name); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
		}
		return c.SendStatus(fiber.StatusOK)
	}

	cfg := &bucket.LoggingConfig{
		TargetBucket: in.LoggingEnabled.TargetBucket,
		TargetPrefix: in.LoggingEnabled.TargetPrefix,
	}
	if err := bucket.PutLogging(name, cfg); err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleGetBucketWebsite(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	cfg, err := bucket.GetWebsite(name)
	if err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchWebsiteConfiguration", "The specified bucket does not have a website configuration", "/"+name)
	}
	out := WebsiteConfigurationXML{Xmlns: xmlNamespace}
	if cfg.IndexDocument != "" {
		out.IndexDocument = &IndexDocumentXML{Suffix: cfg.IndexDocument}
	}
	if cfg.ErrorDocument != "" {
		out.ErrorDocument = &ErrorDocumentXML{Key: cfg.ErrorDocument}
	}
	return writeXML(c, fiber.StatusOK, out)
}

func handlePutBucketWebsite(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty website body", "/"+name)
	}

	var in WebsiteConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}
	if in.IndexDocument == nil || in.IndexDocument.Suffix == "" {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "IndexDocument.Suffix required", "/"+name)
	}

	cfg := &bucket.WebsiteConfig{IndexDocument: in.IndexDocument.Suffix}
	if in.ErrorDocument != nil {
		cfg.ErrorDocument = in.ErrorDocument.Key
	}
	if err := bucket.PutWebsite(name, cfg); err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucketWebsite(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	if err := bucket.DeleteWebsite(name); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func handleGetBucketNotification(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return writeXML(c, fiber.StatusOK, NotificationConfigurationXML{Xmlns: xmlNamespace})
}

func handlePutBucketNotification(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleGetBucketRequestPayment(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return writeXML(c, fiber.StatusOK, RequestPaymentConfigurationXML{Xmlns: xmlNamespace, Payer: "BucketOwner"})
}

func handlePutBucketRequestPayment(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusOK)
}
