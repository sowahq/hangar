# Hangar

Self-hosted object storage in Go. Content-addressed chunks (blake3) with zstd compression on Pebble, served via Fiber.

## Features

- HTTP API with bucket + object semantics
- Per-bucket auth tokens (argon2id) with `read` / `write` / `delete` / `admin` permissions
- Per-bucket quotas (max bytes / max objects)
- HTTP range requests (RFC 7233)
- Optional rate limiting per token / IP
- Deep healthcheck (DB probe, disk free, GC liveness)
- Background garbage collection of unreferenced chunks
- Streaming uploads / downloads (no full-body buffering)
- Graceful shutdown

## Requirements

- Go ≥ 1.25 (see `go.mod`)
- Linux / macOS / Windows
- ~1× data size of disk for storage

## Quickstart

```sh
make build
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start if missing.

### Docker

```sh
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data hangar:dev
```

## Configuration

`config.toml`:

```toml
data_directory = "data"
pprof          = false

[api]
bind_addr = ":8080"

[storage]
chunk_size         = 4194304  # 4 MiB
enable_compression = true

[garbage_collection]
interval_hours = 24

[rate_limit]
enabled     = false
max         = 100
window_sec  = 60
```

## HTTP API

### Admin (no auth — bind to localhost or protect upstream)

| Method | Path                                      | Purpose                       |
|--------|-------------------------------------------|-------------------------------|
| GET    | `/admin/buckets`                          | List buckets                  |
| PUT    | `/admin/buckets/:bucket`                  | Create bucket                 |
| GET    | `/admin/buckets/:bucket`                  | Get bucket info               |
| DELETE | `/admin/buckets/:bucket`                  | Delete empty bucket           |
| PUT    | `/admin/buckets/:bucket/quota`            | Set bucket quota              |
| POST   | `/admin/buckets/:bucket/tokens`           | Create token (returned once)  |
| GET    | `/admin/buckets/:bucket/tokens`           | List token IDs                |
| DELETE | `/admin/buckets/:bucket/tokens/:id`       | Revoke token                  |
| GET    | `/status`                                 | Deep healthcheck              |

### Objects (token required unless bucket is public + GET)

| Method | Path              | Purpose                              |
|--------|-------------------|--------------------------------------|
| GET    | `/:bucket`        | List objects                         |
| PUT    | `/:bucket/*`      | Upload object (`Content-Length` req.)|
| GET    | `/:bucket/*`      | Download object (supports `Range`)   |
| DELETE | `/:bucket/*`      | Delete object                        |

Tokens are passed as `Authorization: Bearer <id>.<secret>`.

### Example

```sh
# Create bucket
curl -X PUT http://localhost:8080/admin/buckets/photos

# Issue token
curl -X POST http://localhost:8080/admin/buckets/photos/tokens \
  -H 'Content-Type: application/json' \
  -d '{"permissions":["read","write"]}'
# → {"token":"abc123.xyz...", "id":"abc123", ...}

# Upload
curl -X PUT http://localhost:8080/photos/holiday/img.jpg \
  -H "Authorization: Bearer abc123.xyz..." \
  --data-binary @img.jpg

# Download (full)
curl -O -H "Authorization: Bearer abc123.xyz..." \
  http://localhost:8080/photos/holiday/img.jpg

# Download (range)
curl -H "Authorization: Bearer abc123.xyz..." \
     -H "Range: bytes=0-1023" \
     http://localhost:8080/photos/holiday/img.jpg
```

## Development

```sh
make test        # run tests
make test-race   # with race detector
make vet         # static analysis
make fmt         # format
make tidy        # go mod tidy
```

## License

Not yet specified.
