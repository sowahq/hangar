---
title: HTTP API
description: Native admin and object endpoints, tokens, quotas, versioning, healthcheck.
---

Hangar's native HTTP API uses a flat path layout: `/:bucket/:objectKey`. Admin routes live under `/admin/*` and are unauthenticated — see [Configuration › Security](/configuration/#security-notes).

## Admin

| Method | Path                                  | Purpose                          |
|--------|---------------------------------------|----------------------------------|
| `GET`  | `/admin/buckets`                      | List buckets                     |
| `PUT`  | `/admin/buckets/:bucket`              | Create bucket                    |
| `GET`  | `/admin/buckets/:bucket`              | Get bucket info                  |
| `DELETE` | `/admin/buckets/:bucket`            | Delete bucket (must be empty)    |
| `PUT`  | `/admin/buckets/:bucket/quota`        | Set bucket quota                 |
| `PUT`  | `/admin/buckets/:bucket/versioning`   | Toggle versioning                |
| `POST` | `/admin/buckets/:bucket/tokens`       | Create token (returned **once**) |
| `GET`  | `/admin/buckets/:bucket/tokens`       | List token IDs                   |
| `DELETE` | `/admin/buckets/:bucket/tokens/:id` | Revoke token                     |
| `POST` | `/admin/s3keys`                       | Create S3 access key             |
| `GET`  | `/admin/s3keys`                       | List S3 access keys              |
| `DELETE` | `/admin/s3keys/:id`                 | Delete S3 access key             |
| `GET`  | `/status`                             | Deep healthcheck                 |

### Create bucket

```sh
curl -X PUT http://localhost:8080/admin/buckets/photos
```

### Create token

```sh
curl -X POST http://localhost:8080/admin/buckets/photos/tokens \
  -H 'Content-Type: application/json' \
  -d '{"permissions":["read","write"]}'
```

Response:

```json
{
  "id": "abc123def456",
  "token": "abc123def456.<secret>",
  "permissions": ["read","write"],
  "created_at": 1715817600000
}
```

The full `token` value is shown **only once**. Permissions: `read`, `write`, `delete`, `admin`.

### Set quota

```sh
curl -X PUT http://localhost:8080/admin/buckets/photos/quota \
  -H 'Content-Type: application/json' \
  -d '{"max_bytes": 1073741824, "max_objects": 10000}'
```

Zero values disable that limit.

## Objects

| Method | Path           | Purpose                              |
|--------|----------------|--------------------------------------|
| `GET`  | `/:bucket`     | List objects (`?prefix=`, `?limit=`) |
| `PUT`  | `/:bucket/*`   | Upload object                        |
| `GET`  | `/:bucket/*`   | Download object (supports `Range`)   |
| `HEAD` | `/:bucket/*`   | Object metadata                      |
| `DELETE` | `/:bucket/*` | Delete object                        |

Auth: `Authorization: Bearer <id>.<secret>`. Public buckets allow `GET` without a token.

### Upload

```sh
curl -X PUT http://localhost:8080/photos/holiday/img.jpg \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: image/jpeg" \
  --data-binary @img.jpg
```

`Content-Length` is required when the bucket has quotas enabled.

### Range download

```sh
curl -H "Authorization: Bearer $TOKEN" \
     -H "Range: bytes=0-1023" \
     http://localhost:8080/photos/holiday/img.jpg
```

Standard RFC 7233 single-range responses (`206 Partial Content` with `Content-Range`).

### Versioning

When versioning is enabled on a bucket, every `PUT` and `DELETE` keeps history. Query parameters:

- `?versions` on `GET /:bucket/<key>` → list versions
- `?versionId=<id>` on `GET` / `DELETE` → target a specific version

## Healthcheck

```sh
curl http://localhost:8080/status
```

Returns `200` if the DB probe, disk-free check, and GC liveness all pass; `503` otherwise.
