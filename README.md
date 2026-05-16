# Hangar

Self-hosted object storage in Go. Content-addressed chunks (blake3 + zstd) on Pebble, served via Fiber. Native HTTP API and S3-compatible API on the same storage.

> Full documentation: **[hangar.mth.lc](https://hangar.mth.lc)**

v0.9.x, single-node. Distribution and erasure coding are planned but not built yet.

## Quickstart

```sh
make build
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start. The HTTP API binds to `:8080`; the S3 API is disabled by default — enable it in `[s3]`.

### Docker

Pre-built multi-arch image on GHCR:

```sh
docker run --rm -p 8080:8080 -v $(pwd)/data:/data ghcr.io/sowahq/hangar:latest
```

Or build locally:

```sh
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data hangar:dev
```

### Pre-built binaries

Linux / macOS / Windows binaries for tagged releases: [github.com/sowahq/hangar/releases](https://github.com/sowahq/hangar/releases).

## Development

```sh
make test        # tests
make test-race   # race detector
make vet         # static analysis
make fmt         # format
```

## License

[AGPL-3.0](LICENSE)
