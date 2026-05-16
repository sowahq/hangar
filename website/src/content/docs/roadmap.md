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
- Prometheus metrics on a separate port

## Next up

Pre-1.0 polish and the next round of S3 surface:

- **Object Lock / WORM** — retention periods, legal hold.
- **Bucket default encryption** (`PUT /:bucket?encryption`).
- **Bucket policy / ACL** — JSON policy doc, evaluated per request.
- **Object tagging.**
- **Virtual-host-style addressing.**
- **`UploadPartCopy`.**
- **Admin UI.**

## Planned (not built)

These are committed-to directions, not "maybe":

- **Distribution + erasure coding.** Multi-node cluster on top of the per-node Pebble engine. Likely Raft for control plane, content-addressed chunks placed via consistent hashing + EC across nodes. Unlocks `PutBucketReplication`, multi-AZ durability, online repair.
- **SSE-KMS.** Provider integration so master keys can live outside the server.
- **Online / hot backup.** Streaming snapshots without stopping the server.

## Known gaps

- `aws-chunked` PUT without `x-amz-decoded-content-length` defaults to `0` instead of failing — minor.
- No `If-Match` / `If-Unmodified-Since` on object GET yet.
- `ListObjectsV2` scan cost is linear in items returned; very large prefixes pay for it.

Open an [issue](https://github.com/sowahq/hangar/issues) if your workflow needs something not listed.
