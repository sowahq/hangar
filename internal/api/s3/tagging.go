package s3

import (
	"encoding/xml"
	"errors"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/gofiber/fiber/v2"
)

type TagXML struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

type TaggingXML struct {
	XMLName xml.Name `xml:"Tagging"`
	Xmlns   string   `xml:"xmlns,attr,omitempty"`
	TagSet  []TagXML `xml:"TagSet>Tag"`
}

func tagsToXML(tags []bucket.Tag) TaggingXML {
	out := TaggingXML{Xmlns: xmlNamespace}
	for _, t := range tags {
		out.TagSet = append(out.TagSet, TagXML{Key: t.Key, Value: t.Value})
	}
	return out
}

func tagsFromXML(in *TaggingXML) []bucket.Tag {
	out := make([]bucket.Tag, 0, len(in.TagSet))
	for _, t := range in.TagSet {
		out = append(out, bucket.Tag{Key: t.Key, Value: t.Value})
	}
	return out
}

func handlePutBucketTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	body := c.Body()
	if len(body) == 0 {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", "empty tagging body", "/"+name)
	}

	var in TaggingXML
	if err := xml.Unmarshal(body, &in); err != nil {
		return writeError(c, fiber.StatusBadRequest, "MalformedXML", err.Error(), "/"+name)
	}

	tags := tagsFromXML(&in)
	if err := bucket.PutBucketTagging(name, tags); err != nil {
		return writeError(c, fiber.StatusBadRequest, "InvalidTag", err.Error(), "/"+name)
	}

	return c.SendStatus(fiber.StatusOK)
}

func handleGetBucketTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	tags, err := bucket.GetBucketTagging(name)
	if err != nil {
		if errors.Is(err, bucket.ErrTaggingNotFound) {
			return writeError(c, fiber.StatusNotFound, "NoSuchTagSet", "no tag set on bucket", "/"+name)
		}
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	return writeXML(c, fiber.StatusOK, tagsToXML(tags))
}

func handleDeleteBucketTagging(c *fiber.Ctx) error {
	name := c.Params("bucket")
	if !hasPerm(c, auth.PermWrite) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}
	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	if err := bucket.DeleteBucketTagging(name); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
