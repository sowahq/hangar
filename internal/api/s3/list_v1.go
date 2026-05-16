package s3

import (
	"strconv"

	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

func handleListObjectsV1(c *fiber.Ctx, name string) error {
	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	marker := c.Query("marker")

	maxKeys, _ := strconv.Atoi(c.Query("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	res, err := object.ListObjectsV2(&object.ListObjectsV2Request{
		Bucket:     name,
		Prefix:     prefix,
		Delimiter:  delim,
		StartAfter: marker,
		MaxKeys:    maxKeys,
	})
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	out := ListBucketResultV1{
		Xmlns:       xmlNamespace,
		Name:        name,
		Prefix:      prefix,
		Marker:      marker,
		MaxKeys:     maxKeys,
		Delimiter:   delim,
		IsTruncated: res.IsTruncated,
	}

	for _, o := range res.Objects {
		out.Contents = append(out.Contents, Contents{
			Key:          o.Key,
			LastModified: formatS3Time(o.CreatedAt),
			ETag:         o.ETag,
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}

	for _, p := range res.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: p})
	}

	if res.IsTruncated && len(out.Contents) > 0 {
		out.NextMarker = out.Contents[len(out.Contents)-1].Key
	}

	return writeXML(c, fiber.StatusOK, out)
}
