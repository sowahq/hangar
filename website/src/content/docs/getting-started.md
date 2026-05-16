---
title: Getting Started
description: Install Hangar, create a bucket, and upload your first object in under a minute.
---

## Requirements

- Go ≥ 1.25 (see `go.mod`)
- Linux / macOS / Windows
- Disk roughly equal to your data size (chunks + Pebble overhead)

## Build

```sh
git clone https://github.com/sowahq/hangar.git
cd hangar
make build
```

The binary lands in `bin/hangar`.

## First run

```sh
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start. By default the HTTP API binds to `:8080` and the S3 API is disabled. See [Configuration](configuration.md) to enable S3 and SSE.

## Create a bucket + token

The admin endpoints are unauthenticated — **bind to localhost or place behind a reverse proxy with auth**.

```sh
curl -X PUT http://localhost:8080/admin/buckets/photos

curl -X POST http://localhost:8080/admin/buckets/photos/tokens \
  -H 'Content-Type: application/json' \
  -d '{"permissions":["read","write"]}'
# → {"token":"<id>.<secret>", "id":"<id>", ...}
```

The token is returned **once**. Store it.

## Upload, download, range

```sh
TOKEN="<id>.<secret>"

# upload
curl -X PUT http://localhost:8080/photos/img.jpg \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @img.jpg

# download
curl -O -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/photos/img.jpg

# range request
curl -H "Authorization: Bearer $TOKEN" \
     -H "Range: bytes=0-1023" \
     http://localhost:8080/photos/img.jpg
```

## Pre-built binaries

Linux / macOS / Windows tarballs (+ SHA256SUMS) for every tagged release: [github.com/sowahq/hangar/releases](https://github.com/sowahq/hangar/releases).

## Docker

Pre-built multi-arch image (`linux/amd64`, `linux/arm64`) on GHCR:

```sh
docker run --rm -p 8080:8080 -v $(pwd)/data:/data ghcr.io/sowahq/hangar:latest
# or pin to a tag
docker run --rm -p 8080:8080 -v $(pwd)/data:/data ghcr.io/sowahq/hangar:v0.9.0
```

Or build locally:

```sh
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data hangar:dev
```

## Next steps

- [Configure](configuration.md) — full `config.toml` reference, S3 + rate limit + SSE master key
- [HTTP API](api/http.md) — admin and object endpoints
- [S3 API](api/s3.md) — SDK setup, supported ops
- [Encryption](sse.md) — SSE-S3 and SSE-C
