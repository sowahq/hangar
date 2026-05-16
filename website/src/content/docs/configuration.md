---
title: Configuration
description: Full config.toml reference and security notes for Hangar.
---

Hangar is configured through a single `config.toml`. The path is passed via `-c`:

```sh
./bin/hangar server -c config.toml
```

A minimal default is written on first start if the file is missing.

## Full reference

```toml
# Where chunks, Pebble DB, and metadata live.
data_directory = "data"

# Enables /debug/pprof endpoints. Localhost only — never expose.
pprof = false

[api]
# HTTP API listen address.
bind_addr = ":8080"

[storage]
# Target chunk size before compression / encryption.
chunk_size = 4194304        # 4 MiB

# zstd compression for stored chunks.
enable_compression = true

[garbage_collection]
# How often the GC sweeps unreferenced chunks.
interval_hours = 24

[rate_limit]
# Per-token (or per-IP if anonymous) sliding window.
enabled    = false
max        = 100
window_sec = 60

[s3]
# Expose the S3-compatible API on a separate port.
enabled   = false
bind_addr = ":9000"
region    = "us-east-1"

[security]
# Server master key for SSE-S3. Base64-encoded, must decode to 32 bytes.
# Empty disables SSE-S3 — PUT with `x-amz-server-side-encryption: AES256` then returns 503.
# Override with the HANGAR_MASTER_KEY env var.
master_key_b64 = ""
```

## Generating a master key

```sh
openssl rand -base64 32
```

Set it via `[security] master_key_b64` or `HANGAR_MASTER_KEY`. The env var wins if both are set. SSE-C does not require the master key.

## Security notes

- The `/admin/*` endpoints are **unauthenticated**. Bind the HTTP API to `127.0.0.1` and put a TLS-terminating reverse proxy with auth in front — or restrict admin routes with your proxy.
- The S3 port can be exposed publicly; it requires SigV4-signed requests against an S3 key.
- The master key in `config.toml` is sensitive. Use file permissions (`chmod 600`) or inject through `HANGAR_MASTER_KEY` from a secret manager.
