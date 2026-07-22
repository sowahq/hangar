package s3

import (
	"sort"
	"strconv"
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

	maxKeys, _ := strconv.Atoi(c.Query("max-keys"))
	if maxKeys <= 0 {
		maxKeys = 1000
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
		Prefix:          prefix,
		KeyMarker:       keyMarker,
		VersionIDMarker: vidMarker,
		MaxKeys:         maxKeys,
		Delimiter:       delim,
	}

	commonPrefixes := map[string]bool{}
	count := 0

	pastMarker := keyMarker == ""

	for _, k := range keys {
		if !pastMarker {
			if k < keyMarker {
				continue
			}
			if k == keyMarker && vidMarker == "" {
				continue
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
			if !pastMarker {
				if k == keyMarker && v.VersionID == vidMarker {
					pastMarker = true
					continue
				}
				continue
			}

			if count >= maxKeys {
				out.IsTruncated = true
				out.NextKeyMarker = k
				out.NextVersionIDMarker = v.VersionID
				break
			}

			isLatest := v.VersionID == headVID
			vid := v.VersionID
			if vid == "" {
				vid = "null"
			}

			if v.IsDeleteMarker {
				out.DeleteMarkers = append(out.DeleteMarkers, DeleteMarkerXML{
					Key:          k,
					VersionID:    vid,
					IsLatest:     isLatest,
					LastModified: formatS3Time(v.CreatedAt),
					Owner:        owner,
				})
			} else {
				out.Versions = append(out.Versions, ObjectVersionXML{
					Key:          k,
					VersionID:    vid,
					IsLatest:     isLatest,
					LastModified: formatS3Time(v.CreatedAt),
					ETag:         v.ETag,
					Size:         v.Size,
					StorageClass: "STANDARD",
					Owner:        owner,
				})
			}

			count++
		}

		pastMarker = true

		if out.IsTruncated {
			break
		}
	}

	cps := make([]string, 0, len(commonPrefixes))
	for p := range commonPrefixes {
		cps = append(cps, p)
	}
	sort.Strings(cps)
	for _, p := range cps {
		out.CommonPrefixes = append(out.CommonPrefixes, CommonPrefix{Prefix: p})
	}

	return writeXML(c, fiber.StatusOK, out)
}
