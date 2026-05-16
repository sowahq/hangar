# Hangar

Self-hosted object storage in Go. Content-addressed chunks (blake3) + zstd compression on Pebble, served via Fiber. Native HTTP API and S3-compatible API.

> Full documentation: **[hangar docs site](#)** _(coming soon, GitHub Pages)_

## Features

- HTTP API and S3-compatible API (path-style, SigV4, multipart, presigned URLs, aws-chunked)
- Server-Side Encryption — SSE-S3 (AES-256-GCM, server master key) and SSE-C (customer key)
- Per-bucket tokens (argon2id) with `read` / `write` / `delete` / `admin` permissions
- Per-bucket quotas, versioning, public-read buckets
- HTTP Range (RFC 7233), streaming I/O, graceful shutdown
- Background GC of unreferenced chunks, deep healthcheck, optional rate limiting

## Quickstart

```sh
make build
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for design and [docs/ROADMAP.md](docs/ROADMAP.md) for status.

### Docker

```sh
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data hangar:dev
```

## Development

```sh
make test        # tests
make test-race   # race detector
make vet         # static analysis
make fmt         # format
```

## License

[AGPL-3.0](LICENSE)
