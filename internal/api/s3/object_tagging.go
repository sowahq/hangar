package s3

import (
	"encoding/xml"
	"errors"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func storageTagsToXML(tags []storage.Tag) TaggingXML {
	out := TaggingXML{Xmlns: xmlNamespace}
	for _, t := range tags {
		out.TagSet = append(out.TagSet, TagXML{Key: t.Key, Value: t.Value})
	}
	return out
}

func storageTagsFromXML(in *TaggingXML) []storage.Tag {
	out := make([]storage.Tag, 0, len(in.TagSet))
	for _, t := range in.TagSet {
		out = append(out, storage.Tag{Key: t.Key, Value: t.Value})
	}
	return out
}

func handleGetObjectTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	versionID := c.Query("versionId")
	tags, err := object.GetObjectTagging(name, key, versionID)
	if err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}

	if versionID != "" {
		c.Set("x-amz-version-id", versionID)
	}
	return writeXML(c, fiber.StatusOK, storageTagsToXML(tags))
}

func handlePutObjectTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty tagging body", "/"+name+"/"+key)
	}

	var in TaggingXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name+"/"+key)
	}

	versionID := c.Query("versionId")
	tags := storageTagsFromXML(&in)
	if err := object.PutObjectTagging(name, key, versionID, tags); err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusBadRequest, "InvalidTag", err.Error(), "/"+name+"/"+key)
	}

	if versionID != "" {
		c.Set("x-amz-version-id", versionID)
	}
	return c.SendStatus(fiber.StatusOK)
}

func handleDeleteObjectTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	versionID := c.Query("versionId")
	if err := object.DeleteObjectTagging(name, key, versionID); err != nil {
		if errors.Is(err, object.ErrObjectNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchKey", err.Error(), "/"+name+"/"+key)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name+"/"+key)
	}

	return c.SendStatus(fiber.StatusNoContent)
}
