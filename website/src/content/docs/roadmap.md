---
title: Roadmap
description: What's done, what's next, what's planned but not built.
---

Hangar is pre-1.0. This page tracks themes; check the [git log](https://github.com/sowahq/hangar/commits/main) for granular history.

## Done

### S3 surface
- SigV4 (`Authorization`, presigned, `aws-chunked`), path-style routing
- `PutObject`, `GetObject` (+ Range, conditional), `HeadObject`, `DeleteObject`, `DeleteObjects`, `CopyObject`, `ListObjectsV2`, `HeadBucket`, `ListBuckets`, `CreateBucket`, `DeleteBucket`
- Multipart upload (`Create` / `Upload` / `Complete` / `Abort` / `List`)
- `?cors` subresource (PUT/GET/DELETE + preflight)
- `?lifecycle` subresource (PUT/GET/DELETE) with scheduled expiration + abort-stale-multipart
- `x-amz-checksum-*` response echo

### Storage & security
- Server-side encryption: SSE-S3 (HKDF + AES-256-GCM) and SSE-C
- SSE-S3 keyring with rotation (`POST /admin/sse/keys/rotate`)
- Per-bucket auth (argon2id tokens), S3 access keys with bucket scoping
- Content-addressed chunks (blake3), zstd compression, pending-chunk tracker against GC races
- Versioning, quotas, public-read buckets

### Operations
- `hangar backup create` / `restore` (Pebble checkpoint + chunk tree)
- `hangar scrub run` + scheduled scrub (re-hash, quarantine corrupt, dangling-ref report)
- Disk safeguards: min free bytes / pct / node cap
- Rate limit, deep healthcheck (`/status`), graceful shutdown
- JSONL audit log with rotation (`/admin/audit`)
- Prometheus metrics on a separate port (incl. cluster gauges)

### Cluster (beta)
- Seed-based dynamic membership (`seeds = ["host:port"]`, self-registering join)
- HRW chunk placement with RF=2 synchronous fan-out
- Key-sharded metadata (HRW on `bucket+key`), primary sync + secondary async fan-out
- Per-primary WAL with on-recovery catchup stream
- Anti-entropy worker (pull missing, prune orphans, manual trigger admin endpoint)
- Replicated system state (S3 keys, buckets, configs, layout, mpu, versions) via Pebble write hook + cold-start BulkSync
- GC / scrub / lifecycle leader-gated on lowest-id alive node
- Drain / remove / status via `hangar cluster node …`
- `tools/clusterinterop/` 13-scenario e2e harness

## Next up

Pre-1.0 polish:

- **Erasure coding `k+m`** — wire `klauspost/reedsolomon`, replace RF=2 hardcode. Storage parity with MinIO.
- **`hangar cluster init`** — scaffold base64 secret + TOML template.
- **TLS on the dRPC layer** — optional `cluster.tls_{cert,key}` so VPN is no longer mandatory.
- **Version handshake** — refuse mixed-major across cluster.
- **24 h sustained soak** — single command to run cluster under load until SIGINT, report regression.
- **Bucket policy / ACL** — JSON policy doc, evaluated per request.
- **SSE-KMS** — provider integration so master keys can live outside the server.
- **Admin UI.**

## Planned (not built)

- **Multi-DC.** Async replication between geographically distinct clusters.
- **Online / hot backup.** Streaming snapshots without stopping the server.
- **Rolling-version upgrade.** Mixed-version cluster operation during upgrade window.
- **Secret rotation** without full restart.

## Known gaps

- `aws-chunked` PUT without `x-amz-decoded-content-length` defaults to `0` instead of failing — minor.
- No `If-Match` / `If-Unmodified-Since` on object GET yet.
- `ListObjectsV2` scan cost is linear in items returned; very large prefixes pay for it.

Open an [issue](https://github.com/sowahq/hangar/issues) if your workflow needs something not listed.
