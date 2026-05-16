---
title: Roadmap
description: What's done, what's next, and the bigger arcs.
---

Hangar is pre-1.0. The roadmap below tracks themes — features marked _Done_ are merged to `main`.

## Done

- HTTP API: buckets, objects, tokens, quotas, versioning, public-read buckets
- S3-compatible API: SigV4 (header + presigned + aws-chunked), path-style routing
- S3 operations: `PutObject`, `GetObject` (+ Range), `HeadObject`, `DeleteObject`, `DeleteObjects` (batch), `CopyObject`, `ListObjectsV2`, multipart upload
- Server-side encryption: SSE-S3 (master key + HKDF) and SSE-C (customer key)
- Per-bucket auth, S3 access keys with bucket scoping
- Content-addressed chunks (blake3) with zstd compression
- Background GC of unreferenced chunks
- Deep healthcheck (`/status`)

## Next up (one of these per sprint)

- **Object Lock / WORM** — retention periods, legal hold. Compliance-oriented, self-contained.
- **Lifecycle rules** — prefix + age expiration, hooks into GC.
- **Bucket policy / ACL** — JSON policy doc, evaluated per request.
- **S3 polish** — virtual-host-style routing, `x-amz-checksum-*` response echo, object tagging.

## Bigger arcs

- **Distribution + erasure coding** — Raft control plane, EC chunk placement, consistent hashing. Multi-sprint. Single-node-only is the current line in the sand.
- **SSE-KMS** — requires a KMS provider integration.
- **Metrics** — Prometheus exposition. Currently deferred; deep healthcheck covers most needs.

## Known gaps

- `aws-chunked` PUT without `x-amz-decoded-content-length` defaults to `0` instead of failing — minor.
- SDK `Response has no supported checksum` warning — Hangar does not yet echo `x-amz-checksum-*` headers. ETag still validates body integrity.

Open an [issue](https://github.com/sowahq/hangar/issues) if your workflow needs something not listed.
