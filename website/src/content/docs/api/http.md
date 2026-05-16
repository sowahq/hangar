---
title: HTTP API
description: Native admin and object endpoints, tokens, quotas, versioning, audit, lifecycle, SSE key rotation.
---

Hangar's native HTTP API uses a flat path layout: `/:bucket/:objectKey`. Admin routes live under `/admin/*` and are **unauthenticated** — see [Configuration › Security](/configuration/#security-notes).

## Healthcheck

```sh
curl http://localhost:8080/status
```

Returns `200` if the Pebble probe, disk-free check, and GC liveness all pass; `503` otherwise.

## Admin — buckets

| Method   | Path                                  | Purpose                          |
|----------|---------------------------------------|----------------------------------|
| `GET`    | `/admin/buckets`                      | List buckets                     |
| `PUT`    | `/admin/buckets/:bucket`              | Create bucket                    |
| `GET`    | `/admin/buckets/:bucket`              | Get bucket info                  |
| `DELETE` | `/admin/buckets/:bucket`              | Delete bucket (must be empty)    |
| `PUT`    | `/admin/buckets/:bucket/quota`        | Set quota (`max_bytes`, `max_objects`) |
| `PUT`    | `/admin/buckets/:bucket/versioning`   | Toggle versioning                |

### Create bucket

```sh
curl -X PUT http://localhost:8080/admin/buckets/photos
```

### Set quota

```sh
curl -X PUT http://localhost:8080/admin/buckets/photos/quota \
  -H 'Content-Type: application/json' \
  -d '{"max_bytes": 1073741824, "max_objects": 10000}'
```

Zero values disable that limit.

## Admin — tokens (HTTP API)

| Method   | Path                                          | Purpose                          |
|----------|-----------------------------------------------|----------------------------------|
| `POST`   | `/admin/buckets/:bucket/tokens`               | Create token (returned **once**) |
| `GET`    | `/admin/buckets/:bucket/tokens`               | List token IDs                   |
| `DELETE` | `/admin/buckets/:bucket/tokens/:id`           | Revoke token                     |

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

The full `token` value is shown **only once** and stored argon2id-hashed. Permissions: `read`, `write`, `delete`, `admin`.

## Admin — S3 keys

| Method   | Path                          | Purpose                  |
|----------|-------------------------------|--------------------------|
| `POST`   | `/admin/s3keys`               | Create S3 access key     |
| `GET`    | `/admin/s3keys`               | List S3 access keys      |
| `DELETE` | `/admin/s3keys/:id`           | Delete S3 access key     |

```sh
curl -X POST http://localhost:8080/admin/s3keys \
  -H 'Content-Type: application/json' \
  -d '{"permissions":["admin"], "buckets":["photos"]}'
```

`buckets` is optional; empty = all buckets.

## Admin — SSE keyring

See [SSE key rotation](/operations/sse-key-rotation/) for the full workflow.

| Method | Path                              | Purpose                                |
|--------|-----------------------------------|----------------------------------------|
| `GET`  | `/admin/sse/keys`                 | List keys (`{id, created_at, active}`) |
| `POST` | `/admin/sse/keys/rotate`          | Generate a new key + set it active     |
| `PUT`  | `/admin/sse/keys/:id/activate`    | Set an existing key as active          |

Existing objects keep their original `SSEKeyID`. Old keys must stay in the ring to keep those objects readable.

## Admin — lifecycle

See [Lifecycle](/operations/lifecycle/) for the rule shape and scheduler config.

| Method | Path                          | Purpose                                |
|--------|-------------------------------|----------------------------------------|
| `POST` | `/admin/lifecycle/run`        | Trigger an immediate lifecycle scan    |

The scheduler runs automatically at `[lifecycle] interval_hours` if `[lifecycle] enabled = true`. The admin endpoint is for one-off triggering / smoke tests.

## Admin — audit

```sh
curl 'http://localhost:8080/admin/audit?limit=200'
```

Returns the last N events from the audit JSONL (`limit` ≤ 1000, default 100). `503 Audit log disabled` if `[audit] enabled = false`. See [Audit](/operations/audit/) for the event shape.

## Objects

| Method   | Path           | Purpose                                  |
|----------|----------------|------------------------------------------|
| `GET`    | `/:bucket`     | List objects (`?prefix=`, `?limit=`)     |
| `PUT`    | `/:bucket/*`   | Upload object                            |
| `POST`   | `/:bucket/*`   | Multipart-form-friendly upload           |
| `GET`    | `/:bucket/*`   | Download object (supports `Range`)       |
| `HEAD`   | `/:bucket/*`   | Object metadata                          |
| `DELETE` | `/:bucket/*`   | Delete object                            |

Auth: `Authorization: Bearer <id>.<secret>`. Public-read buckets allow `GET` without a token.

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

Standard RFC 7233 single-range responses (`206 Partial Content` with `Content-Range`). Multi-range is not supported.

### Versioning

When versioning is enabled on a bucket, every `PUT` and `DELETE` keeps history. Query parameters:

- `?versions` on `GET /:bucket/<key>` → list versions
- `?versionId=<id>` on `GET` / `DELETE` → target a specific version
