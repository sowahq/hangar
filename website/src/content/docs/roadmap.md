---
title: Roadmap
description: What's done, what's next, what's planned but not built.
---

Hangar is pre-1.0. This page tracks themes; check the [git log](https://github.com/sowahq/hangar/commits/main) for granular history.

## Done

### S3 surface
- SigV4 (`Authorization`, presigned, `aws-chunked`), path-style + virtual-host routing
- `PutObject`, `GetObject` (+ Range, conditional headers `If-Match` / `If-None-Match` / `If-Modified-Since` / `If-Unmodified-Since`), `HeadObject`, `DeleteObject`, `DeleteObjects`, `CopyObject`, `UploadPartCopy`, `ListObjectsV2`, `ListObjects` v1, `ListObjectVersions`, `HeadBucket`, `ListBuckets`, `CreateBucket`, `DeleteBucket`
- Multipart upload (`Create` / `Upload` / `Complete` / `Abort` / `List` / `ListParts`)
- `?cors`, `?lifecycle`, `?tagging`, `?versioning`, `?encryption`, `?object-lock`, `?website`, `?logging` subresources (PUT/GET/DELETE)
- Object tagging + retention + legal hold (GOVERNANCE / COMPLIANCE)
- `?attributes`, `x-amz-checksum-*` echo (CRC32/CRC32C/CRC64NVME/SHA1/SHA256)
- POST policy (presigned form upload), presigned PUT/GET

### Storage & security
- Server-side encryption: SSE-S3 (HKDF + AES-256-GCM) and SSE-C
- SSE-S3 keyring with rotation (`POST /admin/sse/keys/rotate`)
- Per-bucket auth (argon2id tokens), S3 access keys with bucket scoping
- Content-addressed chunks (blake3), zstd compression, pending-chunk tracker against GC races
- Versioning, quotas, public-read buckets
- Object Lock (GOVERNANCE / COMPLIANCE) + Legal Hold

### Operations
- `hangar backup create` / `restore` (Pebble checkpoint + chunk tree)
- `hangar scrub run` + scheduled scrub (re-hash, quarantine corrupt, dangling-ref report, EC-shard accounting)
- Disk safeguards: min free bytes / pct / node cap
- Rate limit, deep healthcheck (`/status`), graceful shutdown
- JSONL audit log with rotation (`/admin/audit`)
- Prometheus metrics on a separate port (incl. cluster gauges)

### Cluster (beta)
- Seed-based dynamic membership (`seeds = ["host:port"]`, self-registering join)
- HRW chunk placement with RF=2 synchronous fan-out
- **Reed-Solomon erasure coding `k+m`** — opt-in via `ec_data_shards` + `ec_parity_shards`, stable HRW shard placement, refcount mirroring
- **Zone-aware HRW** — first-pass picks one node per distinct zone, fills remainder by pure HRW
- **Eager rebalancer** — layout-change callback triggers immediate AE pass, single-flighted, counter at `/admin/cluster/status`
- Key-sharded metadata (HRW on `bucket+key`), primary sync + secondary async fan-out
- Per-primary WAL with on-recovery catchup stream
- Anti-entropy worker — pull missing chunks, prune orphans, reconstruct EC shards from any `k` survivors, push orphan shards to new owners on layout change
- **Deep-scrub admin op** — re-encode `k+m` shards, leave-one-out detect, repair silent corruption
- Replicated system state (S3 keys, buckets, configs, layout, mpu, versions) via Pebble write hook + cold-start BulkSync
- GC / scrub / lifecycle leader-gated on lowest-id alive node
- **Secret rotation** — comma-separated `shared_secret_b64 = "<new>,<old>"`, truncated SHA-256 fingerprints at `/admin/cluster/secret/status`
- **Optional dRPC TLS** — `tls_cert / tls_key / tls_ca / tls_server_name`, mutual auth with CA pin
- **Protocol version handshake** — refuses mixed-major peers
- **`hangar cluster init` CLI** — mints secret + scaffolds `[cluster]` TOML block
- Drain / remove / status via `hangar cluster node …`
- `tools/clusterinterop/` 18-scenario e2e harness — adds `ec`, `ec-ae`, `ec-4plus3`, `ec-6plus3`, `zone-spread`, `soak` (with `SOAK_EC` / `SOAK_CHURN`)

## Next up

Pre-1.0 polish, ordered by impact:

- **Real-LAN multi-host validation.** Loopback-only soak is fine for CI; production confidence needs 3-VM and 9-VM runs against the harness.
- **24 h+ steady soak with metrics capture.** Memory growth, Pebble compaction, goroutine count, AE catch-up time under sustained load.
- **`eager_rebalances` as a Prometheus gauge.** Today it surfaces only on the admin HTTP response.
- **Legacy RF=2 → EC re-encode tool.** Enabling EC after data exists currently requires S3 copy-through-PUT. Ship a `hangar cluster reencode` that walks refcount, re-encodes, swaps shards atomically.
- **Bucket policy / ACL.** JSON policy doc, evaluated per request.
- **SSE-KMS.** Provider integration so master keys can live outside the server.
- **Admin UI.**

## Planned (not built)

- **Multi-DC.** Async replication between geographically distinct clusters. Zone-aware HRW handles racks, not DCs.
- **Online / hot backup.** Streaming snapshots without stopping the server.
- **Rolling-version upgrade.** Mixed-version cluster operation during upgrade window.

## Known gaps

- `aws-chunked` PUT without `x-amz-decoded-content-length` defaults to `0` instead of failing — minor.
- `ListObjectsV2` scan cost is linear in items returned; very large prefixes pay for it.
- Bucket replication API (cross-cluster), `SelectObjectContent`, Glacier tiers, Inventory, Analytics, Public Access Block, Transfer Acceleration — out of scope.

Open an [issue](https://github.com/sowahq/hangar/issues) if your workflow needs something not listed.
