---
title: Configuration
description: Full config.toml reference for Hangar.
---

Hangar is configured through a single `config.toml`. The path is passed via `-c`:

```sh
./bin/hangar server -c config.toml
```

A minimal default is written on first start if the file is missing.

## Full reference

```toml
# Where chunks, Pebble DB, metadata, and (optionally) the audit log live.
data_directory = "data"

# Enables /debug/pprof endpoints. Localhost only — never expose.
pprof = false

[api]
# Native HTTP API listen address. Admin endpoints live here.
bind_addr = ":8080"

# Bearer token protecting every /admin/* endpoint.
# Empty = unauthenticated (a warning is logged at startup).
# Override with the HANGAR_ADMIN_TOKEN env var (wins over this file).
admin_token = ""

[tls]
# Serve the native HTTP API and the S3 API over TLS with this certificate pair.
# Both must be set to enable TLS; leave empty to serve plain HTTP
# (e.g. behind a TLS-terminating reverse proxy). Metrics stay plain HTTP.
cert_file = ""
key_file  = ""

[storage]
# Target chunk size before compression / encryption.
chunk_size = 4194304        # 4 MiB

# zstd compression for stored chunks.
enable_compression = true

# Disk safeguards — refuse PUT once any of these would be violated.
# 0 = disabled.
min_free_bytes  = 0         # absolute minimum free bytes on the data filesystem
min_free_pct    = 0         # minimum free percentage of the data filesystem
node_max_bytes  = 0         # cap on bytes used by this node's data directory

# fsync every metadata write to Pebble. Default true (durable).
# Set false to trade durability for throughput — recent writes may be lost
# on power loss / hard kill. Safe on a UPS-backed host or for non-critical data.
sync_writes = true

[garbage_collection]
# How often the GC sweeps unreferenced chunks (refcount == 0).
interval_hours = 24

[scrub]
# Background integrity scrub. interval_hours = 0 disables it.
# rate_bytes_per_sec throttles disk reads; 0 = unlimited.
interval_hours      = 0
rate_bytes_per_sec  = 0

[rate_limit]
# Per-token (or per-IP if anonymous) sliding window on the native HTTP API.
enabled    = false
max        = 100
window_sec = 60

[s3]
# Expose the S3-compatible API on a separate port.
enabled   = false
bind_addr = ":9000"
region    = "us-east-1"

# Optional virtual-host addressing. When set, requests with
# `Host: <bucket>.<virtual_host_base>` route as `/<bucket><path>`.
# Path-style continues to work in parallel. Leave empty to disable.
# See /operations/virtual-host/
virtual_host_base = ""

[security]
# Server master key for SSE-S3. Base64-encoded, must decode to 32 bytes.
# Empty disables SSE-S3 — PUT with `x-amz-server-side-encryption: AES256` returns 503.
# Override with the HANGAR_MASTER_KEY env var (wins over this file).
master_key_b64 = ""

[metrics]
# Prometheus endpoint on its own port (so you can firewall it separately).
# Exposes hangar_* metrics plus the standard process/go collectors.
enabled   = false
bind_addr = ":9100"

[audit]
# JSONL audit log with size + age rotation.
# path defaults to <data_directory>/audit.log when empty.
enabled         = false
path            = ""
max_size_mb     = 100
max_backups     = 5
retention_days  = 30

[lifecycle]
# Scheduled lifecycle runner: expires objects and aborts stale multipart uploads
# according to the per-bucket lifecycle XML config.
enabled        = false
interval_hours = 24
```

## Generating a master key

```sh
openssl rand -base64 32
```

Set it via `[security] master_key_b64` or `HANGAR_MASTER_KEY`. The env var wins if both are set. SSE-C does not require the master key.

The first server boot with a master key configured seeds a default entry in the SSE keyring under id `default`. Rotate later with `POST /admin/sse/keys/rotate` — see [SSE key rotation](/operations/sse-key-rotation/).

## Disk safeguards

When any of `min_free_bytes`, `min_free_pct`, or `node_max_bytes` is set, every `PutObject` (native + S3) checks the data filesystem before accepting the body and returns a 507-style error if the request would push past the threshold. Set them on production deployments — a full Pebble store can corrupt on the next write.

## Admin API authentication

Set `[api] admin_token` (or `HANGAR_ADMIN_TOKEN`) to require `Authorization: Bearer <token>` on every `/admin/*` endpoint. Generate one with:

```sh
openssl rand -hex 32
```

The CLI picks the token up automatically from the `HANGAR_ADMIN_TOKEN` env var. With no token configured the admin API stays open (previous behaviour) and the server logs a warning at startup.

## Health check

`GET /healthz` on the native HTTP port returns `200 {"status":"ok"}` without auth or rate limiting — point Docker `HEALTHCHECK`, Kubernetes probes, or your uptime monitor at it.

## Security notes

- Protect the `/admin/*` endpoints: set `admin_token` (see above), and/or bind the HTTP API to `127.0.0.1` behind a TLS-terminating reverse proxy.
- The S3 port can be exposed publicly; every request must carry a valid SigV4 signature against an `S3Key`.
- The master key in `config.toml` is sensitive. Use file permissions (`chmod 600`) or inject through `HANGAR_MASTER_KEY` from a secret manager.
- The audit log path is `chmod 0600` by default. Keep it that way.
