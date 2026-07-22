package s3

import (
	"sort"
	"strings"

	"github.com/sowahq/hangar/internal/service/auth"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/sowahq/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
)

func handleListObjectVersions(c *fiber.Ctx) error {
	name := c.Params("bucket")

	if !hasPerm(c, auth.PermRead) || !keyAllowsBucket(c, name) {
		return writeError(c, fiber.StatusForbidden, "AccessDenied", "Access denied", "/"+name)
	}

	if _, err := bucket.GetBucket(name); err != nil {
		return writeError(c, fiber.StatusNotFound, "NoSuchBucket", err.Error(), "/"+name)
	}

	prefix := c.Query("prefix")
	delim := c.Query("delimiter")
	keyMarker := c.Query("key-marker")
	vidMarker := c.Query("version-id-marker")

	enc, encOK := listEncoding(c)
	if !encOK {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "invalid encoding-type", "/"+name)
	}

	maxKeys, ok := parseMaxKeys(c)
	if !ok {
		return writeError(c, fiber.StatusBadRequest, "InvalidArgument", "max-keys must be a non-negative integer", "/"+name)
	}

	if maxKeys == 0 {
		return writeXML(c, fiber.StatusOK, ListVersionsResult{
			Xmlns:           xmlNamespace,
			Name:            name,
			Prefix:          encodeListValue(enc, prefix),
			KeyMarker:       encodeListValue(enc, keyMarker),
			VersionIDMarker: vidMarker,
			MaxKeys:         maxKeys,
			Delimiter:       encodeListValue(enc, delim),
			EncodingType:    enc,
		})
	}

	metas, _, err := storage.ScanBucketVersions(name)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "InternalError", err.Error(), "/"+name)
	}

	byKey := map[string][]*storage.Metadatas{}
	for _, m := range metas {
		if prefix != "" && !strings.HasPrefix(m.Key, prefix) {
			continue
		}
		byKey[m.Key] = append(byKey[m.Key], m)
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	owner := Owner{ID: currentKey(c), DisplayName: currentKey(c)}

	out := ListVersionsResult{
		Xmlns:           xmlNamespace,
		Name:            name,
		Prefix:          encodeListValue(enc, prefix),
		KeyMarker:       encodeListValue(enc, keyMarker),
		VersionIDMarker: vidMarker,
		MaxKeys:         maxKeys,
		Delimiter:       encodeListValue(enc, delim),
		EncodingType:    enc,
	}

	commonPrefixes := map[string]bool{}
	count := 0

	pastMarker := keyMarker == ""
	var lastKey, lastVID string

	for _, k := range keys {
		if !pastMarker {
			if k < keyMarker {
				continue
			}
			if k == keyMarker && vidMarker == "" {
				continue
			}
			if k > keyMarker {
				pastMarker = true
			}
		}

		if delim != "" {
			rest := strings.TrimPrefix(k, prefix)
			if idx := strings.Index(rest, delim); idx >= 0 {
				cp := prefix + rest[:idx+len(delim)]
				commonPrefixes[cp] = true
				continue
			}
		}

		versions := byKey[k]
		sort.SliceStable(versions, func(i, j int) bool {
			return versions[i].CreatedAt > versions[j].CreatedAt
		})

		var headVID string
		if head, herr := storage.GetMetadataFromBucket(name, k); herr == nil && head != nil {
			headVID = head.VersionID
		}

		for _, v := range versions {
			vid := v.VersionID
			if vid == "" {
				vid = "null"
			}

			if !pastMarker {
				if k == keyMarker && vid == vidMarker {
					pastMarker = true
				}
				continue
			}

			if count >= maxKeys {
				out.IsTruncated = true
				break
			}

			isLatest := v.VersionID == headVID

			if v.IsDeleteMarker {
				out.DeleteMarkers = append(out.DeleteMarkers, DeleteMarkerXML{
					Key:          encodeListValue(enc, k),
					VersionID:    vid,
					IsLatest:     isLatest,
					LastModified: formatS3Time(v.CreatedAt),
					Owner:        owner,
				})
			} else {
				out.Versions = append(out.Versions, ObjectVersionXML{
					Key:          encodeListValue(enc, k),
					VersionID:    vid,
					IsLatest:     isLatest,
					LastModified: formatS3Time(v.CreatedAt),
					ETag:         v.ETag,
					Size:         v.Size,
					StorageClass: "STANDARD",
					Owner:        owner,
				})
			}

			lastKey = k
			lastVID = vid
			count++
		}

		pastMarker = true

		if out.IsTruncated {
			break
		}
	}

	if out.IsTruncated {
		out.NextKeyMarker = encodeListValue(enc, lastKey)
		out.NextVersionIDMarker = lastVID
	}

	cps := make([]string, 0, len(commonPrefixes))
	for p := range commonPrefixes {
		cps = append(cps, p)
	}
	sort.Strings(cps)
	for _, p := range cps {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: encodeListValue(enc, p)})
	}

	return writeXML(c, fiber.StatusOK, out)
}
