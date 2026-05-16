# Hangar

Self-hosted object storage in Go. Content-addressed chunks (blake3) + zstd compression on Pebble, served via Fiber. Native HTTP API and S3-compatible API.

> Full documentation: **[hangar.mth.lc](https://hangar.mth.lc)**

## Features

- HTTP API and S3-compatible API (path-style, SigV4, multipart, presigned URLs, aws-chunked)
- Server-Side Encryption — SSE-S3 (AES-256-GCM, server master key) and SSE-C (customer key)
- Per-bucket tokens (argon2id) with `read` / `write` / `delete` / `admin` permissions
- Per-bucket quotas, versioning, public-read buckets
- HTTP Range (RFC 7233), streaming I/O, graceful shutdown
- Background GC of unreferenced chunks, deep healthcheck, optional rate limiting

## Limitations

- **Single-node**: no replication, no distribution, no erasure coding (yet).
- **No SSE-KMS** and no key rotation; SSE breaks cross-object dedup (intra-object only).
- **No bucket default encryption** (`PUT /:bucket?encryption`).
- **CopyObject across different SSE keys** requires full decrypt + re-encrypt (inherent to AEAD: changing key or nonce produces different ciphertext, so chunks cannot be reused). Same-key copies stay cheap.
- **No Prometheus metrics** yet (planned).
- **Pre-1.0**: API surface and on-disk format may change.

Full status on [hangar.mth.lc](https://hangar.mth.lc).

## Quickstart

```sh
make build
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start. See [hangar.mth.lc](https://hangar.mth.lc) for design and status.

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
