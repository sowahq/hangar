---
title: Architecture
description: Layers, write/read paths, multipart encoding, GC, scrub, lifecycle, audit.
---

Hangar is a single-binary daemon. All persistent state lives under `data_directory`.

## Layers

```
┌──────────────────────────────────────────────────────────────┐
│ CLI ( cmd/ )                                                 │
│   hangar server | bucket | s3keys | backup | scrub           │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────────┐
│ API layer                                                    │
│   internal/api/http     — native admin + object API          │
│   internal/api/s3       — SigV4-authenticated S3 API         │
│   internal/api/metrics  — Prometheus (opt-in)                │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────────┐
│ Service layer                                                │
│   bucket | object | auth | gc | scrub | lifecycle |          │
│   sse(keyring) | audit | backup | metrics | diskspace        │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────┬───────────┴────────────┬───────────────────────────┐
│ Chunker  │ Pebble KV              │  Crypto (AEAD + HKDF)     │
│ blake3   │ buckets, objects,      │  pkg/crypto/aead.go       │
│ zstd     │ chunkrefs, mpu, tokens │                           │
│          │ ssekr, cors, lifecycle │                           │
└────┬─────┴────────────────────────┴───────────────────────────┘
     │
┌────▼─────────────────────────────────────────────────────────┐
│ Filesystem chunks (content-addressed)                        │
│   data/chunks/<aa>/<bb>/<blake3-hex>                         │
│   data/chunks/.corrupted/  (quarantine, populated by scrub)  │
└──────────────────────────────────────────────────────────────┘
```

The S3 router and the native HTTP router are separate Fiber apps, listening on separate ports, sharing the service layer.

## Write path (`PUT object`)

1. Probe content-type from first 4 KiB if not provided.
2. Enforce disk safeguards (`min_free_bytes`, `min_free_pct`, `node_max_bytes`) and quota.
3. If SSE requested, derive per-object key (HKDF for SSE-S3, copy raw key for SSE-C). For SSE-S3, the active keyring entry's ID is recorded in metadata.
4. Stream the body through the chunker: split into `chunk_size` pieces.
5. For each chunk: zstd-compress → AEAD-seal (if SSE) → blake3-hash → write `data/chunks/aa/bb/<hash>` atomically (`tmp + rename`) if absent → mark pending → `IncrementChunkRefs`.
6. Compute global blake3 over plaintext → ETag.
7. Write `Metadatas{ETag, Size, ChunkHashes, SSE…, Checksum…}` to Pebble under `metadata:<bucket>/<key>`.
8. Clear pending markers.

## Read path (`GET object`)

1. Load metadata.
2. If SSE-C, validate customer headers against stored MD5.
3. If SSE-S3, look up `SSEKeyID` in the keyring (defaults to `default`) and derive per-object key from master + stored salt.
4. Stream chunk-by-chunk: open file → AEAD-open → zstd-decode → emit.
5. `Range` requests skip leading bytes inside the first chunk and `LimitReader` the tail.

## Multipart

- **Initiate** writes a `MultipartHeader` and assigns a 16-byte hex `UploadID`. SSE config (including `SSEKeyID`) is captured here so every part inherits.
- **UploadPart** chunks the part body, encrypts with `(uploadNoncePrefix, partNumber<<40 | localIdx)` to avoid nonce reuse across parts.
- **Complete** concatenates part chunk lists into a single `Metadatas`. For SSE objects, parallel `SSEPartNumbers` + `SSEPartChunkCounts` arrays let the reader recover `(partNumber, localIdx)` from a global chunk index.

## Garbage collection

A background loop sweeps `chunkref:` entries with count `0` and deletes the matching files. `IncrementChunkRefs` / `DecrementChunkRefs` are taken under a single mutex. A pending-chunk tracker prevents the upload/GC race for chunks written but not yet referenced. Bootstrap reconstructs counts from metadata at startup if a `chunkref:` snapshot is missing.

## Integrity scrub

`hangar scrub run` (or the scheduled scrub if `[scrub] interval_hours > 0`):

- Walks `data/chunks/`, re-hashes each chunk, compares with the filename.
- Moves mismatched files to `data/chunks/.corrupted/` (unless `--dry-run`).
- Detects dangling `chunkref:` entries pointing at missing files.
- Reports `{Corrupted, Quarantined, MissingFiles, DanglingRefs, BytesScanned}`. Counters are exposed via Prometheus (`hangar_scrub_*`).

## Lifecycle

Per-bucket XML rules persisted under `lifecycle:<bucket>`. A scheduler runs every `[lifecycle] interval_hours` and:

- Deletes objects older than `Expiration.Days` whose `Key` matches the rule's `Prefix` (longest-prefix-wins).
- Aborts multipart uploads older than `AbortIncompleteMultipartUpload.DaysAfterInitiation`.

The admin endpoint `POST /admin/lifecycle/run` triggers an immediate scan.

## CORS

Per-bucket XML config persisted under `cors:<bucket>`. The S3 router has a global preflight handler (`OPTIONS`) and a response middleware that adds `Access-Control-*` headers to cross-origin actual requests. Origin patterns support `*` and glob (`*.example.com`).

## Audit log

When `[audit] enabled = true`, every admin action and significant system event appends a JSONL record to `audit.log`. Rotated by size (`max_size_mb`) and pruned by count (`max_backups`) and age (`retention_days`). Reads via `/admin/audit?limit=N`.

## SSE keyring

Pebble keys `ssekr:keys:<id>` (raw 32-byte key + creation time) and `ssekr:active` (current active key id). Bootstrap on startup seeds `default` from `[security] master_key_b64` if absent. Rotation creates a new random key and switches `ssekr:active`. Existing objects record the key id they were sealed under (`Metadatas.SSEKeyID`), so old keys must remain in the ring for their objects to remain readable.

## Authentication

- **HTTP API** — per-bucket tokens stored as argon2id(secret). `Authorization: Bearer <id>.<secret>`.
- **S3 API** — SigV4 over an `S3Key` (access key + raw secret). Permissions: `read`, `write`, `delete`, `admin`. Keys can be restricted to a list of buckets.
- **Admin** — `/admin/*` is unauthenticated by design; place behind a reverse proxy.

## What lives where in Pebble

| Kind                       | Key prefix          |
|----------------------------|---------------------|
| Bucket info                | `bucket:`           |
| Object metadata            | `metadata:`         |
| Object version metadata    | `version:`          |
| Chunk refcount             | `chunkref:`         |
| Multipart header           | `mpu:`              |
| Multipart part             | `mpupart:`          |
| Bucket token               | `auth:bucket:…`     |
| S3 access key              | `auth:s3keys:…`     |
| SSE keyring entry          | `ssekr:keys:…`      |
| SSE active key pointer     | `ssekr:active`      |
| Per-bucket CORS config     | `cors:<bucket>`     |
| Per-bucket lifecycle config| `lifecycle:<bucket>`|

Chunks themselves are on the filesystem at `data/chunks/<aa>/<bb>/<blake3-hex>`. Quarantined chunks live under `data/chunks/.corrupted/`. The audit log is `data/audit.log` by default (configurable).
