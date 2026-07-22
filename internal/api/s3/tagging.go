package s3

import (
	"encoding/xml"
	"errors"
	"net/url"
	"strconv"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func parseTaggingHeader(h string) ([]storage.Tag, error) {
	if h == "" {
		return nil, nil
	}
	vals, err := url.ParseQuery(h)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Tag, 0, len(vals))
	for k, v := range vals {
		val := ""
		if len(v) > 0 {
			val = v[0]
		}
		out = append(out, storage.Tag{Key: k, Value: val})
	}
	if err := bucket.ValidateTags(out); err != nil {
		return nil, err
	}
	return out, nil
}

func writeTaggingCount(c *fiber.Ctx, tags []storage.Tag) {
	if len(tags) > 0 {
		c.Set("x-amz-tagging-count", strconv.Itoa(len(tags)))
	}
}

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
