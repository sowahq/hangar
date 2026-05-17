---
title: Conditional requests
description: ETag and Last-Modified preconditions for GET, HEAD, PUT, CopyObject, and UploadPartCopy.
---

Hangar honors the standard S3 conditional headers for safe concurrent reads, optimistic-concurrency writes, and conditional copies.

## GET / HEAD

| Header                  | Behavior                                         |
|-------------------------|--------------------------------------------------|
| `If-Match: <etag>`      | 412 PreconditionFailed if ETag mismatch          |
| `If-None-Match: <etag>` | 304 Not Modified if ETag matches                 |
| `If-Modified-Since: <date>`   | 304 if not modified since                  |
| `If-Unmodified-Since: <date>` | 412 if modified since                      |

`<etag>` accepts quoted, unquoted, `W/` weak prefix, and `*` (matches any). Multiple values may be comma-separated.

Dates parse RFC1123 / `http.TimeFormat`. Comparison is at second precision.

## PUT (object create / overwrite)

| Header                  | Behavior                                         |
|-------------------------|--------------------------------------------------|
| `If-Match: <etag>`      | 412 if object missing OR ETag mismatch (atomic CAS) |
| `If-None-Match: *`      | 412 if object already exists (create-only)       |

Use cases:

- **CAS update**: `PUT … If-Match: "<oldETag>"` to safely update only if no concurrent writer beat you.
- **Create-only**: `PUT … If-None-Match: *` to avoid overwriting an existing key (e.g. idempotent uploads, write-once semantics).

## CopyObject / UploadPartCopy

Preconditions applied to the **source** object. Mismatch yields `412 PreconditionFailed` and no copy happens.

| Header                                       | Behavior                              |
|----------------------------------------------|---------------------------------------|
| `x-amz-copy-source-if-match`                 | Source ETag must match                |
| `x-amz-copy-source-if-none-match`            | Source ETag must NOT match            |
| `x-amz-copy-source-if-modified-since`        | Source must have been modified since  |
| `x-amz-copy-source-if-unmodified-since`      | Source must NOT have been modified since |

## Example: safe overwrite

```bash
ETAG=$(curl -sI ... | grep -i ETag | awk '{print $2}' | tr -d '\r')
curl -X PUT ... -H "If-Match: $ETAG" -d 'new content'
# 200 → write succeeded
# 412 → someone else wrote first; refetch & retry
```
