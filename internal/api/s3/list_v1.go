package s3

import (
	"github.com/sowahq/hangar/internal/service/object"
	"github.com/gofiber/fiber/v2"
)

func handleListObjectsV1(c *fiber.Ctx, name string) error {
	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	marker := c.Query("marker")

	enc, encOK := listEncoding(c)
	if !encOK {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "invalid encoding-type", "/"+name)
	}

	maxKeys, ok := parseMaxKeys(c)
	if !ok {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "max-keys must be a non-negative integer", "/"+name)
	}

	out := ListBucketResultV1{
		Xmlns:        xmlNamespace,
		Name:         name,
		Prefix:       encodeListValue(enc, prefix),
		Marker:       encodeListValue(enc, marker),
		MaxKeys:      maxKeys,
		Delimiter:    encodeListValue(enc, delim),
		EncodingType: enc,
	}

	if maxKeys == 0 {
		return writeXML(c, fiber.StatusOK, out)
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

	out.IsTruncated = res.IsTruncated

	for _, o := range res.Objects {
		out.Contents = append(out.Contents, Contents{
			Key:          encodeListValue(enc, o.Key),
			LastModified: formatS3Time(o.CreatedAt),
			ETag:         o.ETag,
			Size:         o.Size,
			StorageClass: "STANDARD",
		})
	}

	for _, p := range res.CommonPrefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: encodeListValue(enc, p)})
	}

	if res.IsTruncated {
		var nm string
		if len(res.Objects) > 0 {
			nm = res.Objects[len(res.Objects)-1].Key
		}
		if len(res.CommonPrefixes) > 0 {
			if cp := res.CommonPrefixes[len(res.CommonPrefixes)-1]; cp > nm {
				nm = cp
			}
		}
		out.NextMarker = encodeListValue(enc, nm)
	}

	return writeXML(c, fiber.StatusOK, out)
}
