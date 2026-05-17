---
title: Object versioning
description: Enable versioning, list versions, restore previous versions, and inspect delete markers.
---

Hangar supports per-bucket object versioning. When enabled, every PUT/DELETE produces a new version instead of overwriting in place. Deletes insert a "delete marker" as the latest version; reads still succeed against prior versions by passing `versionId`.

## Enable versioning

Standard S3 XML on `PUT /:bucket?versioning`:

```xml
<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Status>Enabled</Status>
</VersioningConfiguration>
```

Or via the native admin API:

```bash
curl -X PUT "http://localhost:8080/admin/buckets/mybucket/versioning" \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```

`Status: Suspended` flips the flag off. New writes stop creating versions but existing versions remain.

## Read current config

```
GET /:bucket?versioning
```

Returns `<VersioningConfiguration><Status>Enabled</Status></VersioningConfiguration>` or an empty `<VersioningConfiguration/>` if versioning was never enabled.

## List versions

```
GET /:bucket?versions
```

Query parameters: `prefix`, `delimiter`, `key-marker`, `version-id-marker`, `max-keys`.

Response interleaves `<Version>` and `<DeleteMarker>` entries. Each entry carries `Key`, `VersionId`, `IsLatest`, `LastModified` (and for non-marker versions: `ETag`, `Size`, `StorageClass`).

Pagination uses `NextKeyMarker` + `NextVersionIdMarker` when `IsTruncated` is true.

## Read a specific version

```
GET  /:bucket/:key?versionId=<id>
HEAD /:bucket/:key?versionId=<id>
```

A read against the current version (no `versionId`) returns `404 NoSuchKey` if a delete marker tops the version stack. Pass the explicit `versionId` to restore-read older data.

## Delete a specific version

```
DELETE /:bucket/:key?versionId=<id>
```

Deleting an explicit version removes only that version. Deleting without `versionId` on a versioned bucket inserts a new delete marker.

## Interaction with Object Lock

Object Lock requires versioning to be enabled on the bucket. See [Object Lock](/operations/object-lock/).
