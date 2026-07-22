package s3

import (
	"encoding/xml"
	"errors"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

type SSEByDefault struct {
	SSEAlgorithm   string `xml:"SSEAlgorithm"`
	KMSMasterKeyID string `xml:"KMSMasterKeyID,omitempty"`
}

type SSERuleXML struct {
	ApplyServerSideEncryptionByDefault SSEByDefault `xml:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled                   bool         `xml:"BucketKeyEnabled,omitempty"`
}

type ServerSideEncryptionConfigurationXML struct {
	XMLName xml.Name     `xml:"ServerSideEncryptionConfiguration"`
	Xmlns   string       `xml:"xmlns,attr,omitempty"`
	Rules   []SSERuleXML `xml:"Rule"`
}

func toEncryptionXML(cfg *bucket.EncryptionConfig) ServerSideEncryptionConfigurationXML {
	return ServerSideEncryptionConfigurationXML{
		Xmlns: xmlNamespace,
		Rules: []SSERuleXML{{
			ApplyServerSideEncryptionByDefault: SSEByDefault{
				SSEAlgorithm:   cfg.Algorithm,
				KMSMasterKeyID: cfg.KMSKeyID,
			},
		}},
	}
}

func fromEncryptionXML(in *ServerSideEncryptionConfigurationXML) (*bucket.EncryptionConfig, error) {
	if len(in.Rules) == 0 {
		return nil, errors.New("at least one Rule required")
	}

	r := in.Rules[0].ApplyServerSideEncryptionByDefault
	if r.SSEAlgorithm != "AES256" {
		return nil, errors.New("unsupported SSEAlgorithm: only AES256 is supported")
	}

	return &bucket.EncryptionConfig{
		Algorithm: r.SSEAlgorithm,
		KMSKeyID:  r.KMSMasterKeyID,
	}, nil
}

func handleGetBucketEncryption(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	cfg, err := bucket.GetEncryption(name)
	if err != nil {
		if errors.Is(err, bucket.ErrEncryptionNotFound) {
			return writeError(c, fiber.StatusNotFound, "ServerSideEncryptionConfigurationNotFoundError", "The server side encryption configuration was not found", "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return writeXML(c, fiber.StatusOK, toEncryptionXML(cfg))
}

func handlePutBucketEncryption(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty encryption body", "/"+name)
	}

	var in ServerSideEncryptionConfigurationXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	cfg, err := fromEncryptionXML(&in)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", err.Error(), "/"+name)
	}

	if err := bucket.PutEncryption(name, cfg); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteBucketEncryption(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	if err := bucket.DeleteEncryption(name); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func applyBucketDefaultSSE(bucketName string, sseReq *object.SSERequest) *object.SSERequest {
	if sseReq != nil {
		return sseReq
	}

	cfg, err := bucket.GetEncryption(bucketName)
	if err != nil || cfg == nil {
		return nil
	}

	if cfg.Algorithm != "AES256" {
		return nil
	}

	return &object.SSERequest{Algorithm: object.SSEAlgoS3}
}
