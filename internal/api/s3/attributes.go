package s3

import (
	"net/http"
	"strings"
	"time"

	"github.com/anhostfr/hangar/internal/service/auth"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/service/object"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func handleGetObjectAttributes(c *fiber.Ctx) error {
	name := c.Params("bucket")
	key := c.Params("*")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name+"/"+key)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	versionID := c.Query("versionId")

	var m *storage.Metadatas
	var err error
	if versionID != "" {
		m, err = object.GetVersionMetadata(name, key, versionID)
	} else {
		m, err = object.GetMetadata(name, key)
	}
	if err != nil || m == nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+name+"/"+key)
	}

	if m.IsDeleteMarker {
		c.Set("x-amz-delete-marker", "true")
		return writeError(c, fiber.StatusNotFound, "NoSuchKey", "object not found", "/"+name+"/"+key)
	}

	wanted := parseAttributeHeader(c)

	out := GetObjectAttributesOutput{Xmlns: xmlNamespace}

	if wanted["ETag"] {
		out.ETag = strings.Trim(m.ETag, `"`)
	}
	if wanted["StorageClass"] {
		out.StorageClass = "STANDARD"
	}
	if wanted["ObjectSize"] {
		size := m.Size
		out.ObjectSize = &size
	}
	if wanted["Checksum"] && m.ChecksumAlgorithm != "" {
		ck := &ChecksumXML{ChecksumType: "FULL_OBJECT"}
		switch strings.ToUpper(m.ChecksumAlgorithm) {
		case "CRC32":
			ck.ChecksumCRC32 = m.ChecksumValue
		case "CRC32C":
			ck.ChecksumCRC32C = m.ChecksumValue
		case "SHA1":
			ck.ChecksumSHA1 = m.ChecksumValue
		case "SHA256":
			ck.ChecksumSHA256 = m.ChecksumValue
		case "CRC64NVME":
			ck.ChecksumCRC64NVME = m.ChecksumValue
		}
		out.Checksum = ck
	}
	if wanted["ObjectParts"] {
		parts := 0
		if len(m.SSEPartChunkCounts) > 0 {
			parts = len(m.SSEPartChunkCounts)
		}
		out.ObjectParts = &ObjectPartsXML{PartsCount: parts}
	}

	c.Set("Last-Modified", time.UnixMilli(m.CreatedAt).UTC().Format(http.TimeFormat))
	if m.VersionID != "" {
		c.Set("x-amz-version-id", m.VersionID)
	}

	return writeXML(c, fiber.StatusOK, out)
}

func parseAttributeHeader(c *fiber.Ctx) map[string]bool {
	out := map[string]bool{}
	values := c.Request().Header.PeekAll("x-amz-object-attributes")
	combined := ""
	for i, v := range values {
		if i > 0 {
			combined += ","
		}
		combined += string(v)
	}
	if combined == "" {
		for _, k := range []string{"ETag", "Checksum", "ObjectParts", "StorageClass", "ObjectSize"} {
			out[k] = true
		}
		return out
	}
	for _, p := range strings.Split(combined, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}
