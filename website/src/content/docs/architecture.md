---
title: Architecture
description: Layers, write/read paths, multipart encoding, GC, and on-disk layout.
---

Hangar is a single-binary daemon. All persistent state lives under `data_directory`.

## Layers

```
┌──────────────────────────────────────────────────────────────┐
│ CLI ( cmd/ )                                                 │
│   hangar server | bucket | s3keys                            │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────────┐
│ API layer                                                    │
│   internal/api/http  — native admin + object API             │
│   internal/api/s3    — SigV4-authenticated S3-compatible API │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────────────────▼───────────────────────────────────────┐
│ Service layer                                                │
│   bucket  |  object  |  auth  |  gc                          │
└──────────────────────┬───────────────────────────────────────┘
                       │
┌──────────┬───────────┴────────────┬───────────────────────────┐
│ Chunker  │ Pebble KV (metadata)   │  Crypto (AEAD + HKDF)     │
│ blake3   │ buckets, objects,      │  pkg/crypto/aead.go       │
│ zstd     │ chunkrefs, mpu, tokens │                           │
└────┬─────┴────────────────────────┴───────────────────────────┘
     │
┌────▼─────────────────────────────────────────────────────────┐
│ Filesystem chunks (content-addressed)                        │
│   data/chunks/<aa>/<bb>/<blake3-hex>                         │
└──────────────────────────────────────────────────────────────┘
```

## Write path (`PUT object`)

1. Probe content-type from first 4 KiB if not provided.
2. If SSE requested, derive per-object key (HKDF for SSE-S3, copy raw key for SSE-C).
3. Stream the body through the chunker: split into `chunk_size` pieces.
4. For each chunk: zstd-compress → AEAD-seal (if SSE) → blake3-hash → write `data/chunks/aa/bb/<hash>` if absent → record `ChunkRef++`.
5. Compute global blake3 over plaintext → ETag.
6. Write `Metadatas{ETag, Size, ChunkHashes, SSE…}` to Pebble under `metadata:<bucket>/<key>`.

## Read path (`GET object`)

1. Load metadata.
2. If SSE-C, validate customer headers against stored MD5.
3. If SSE-S3, derive per-object key from master + stored salt.
4. Stream chunk-by-chunk: open file → AEAD-open → zstd-decode → emit.
5. `Range` requests skip leading bytes inside the first chunk and `LimitReader` the tail.

## Multipart

- `Initiate` writes a `MultipartHeader` and assigns a 16-byte hex `UploadID`. SSE config is captured here so every part inherits.
- `UploadPart` chunks the part body, encrypts with `(uploadNoncePrefix, partNumber<<40 | localIdx)` to avoid nonce reuse across parts.
- `Complete` concatenates part chunk lists into a single `Metadatas`. For SSE objects, parallel `SSEPartNumbers` + `SSEPartChunkCounts` arrays let the reader recover `(partNumber, localIdx)` from a global chunk index.

## Garbage collection

A background loop sweeps `chunkref:` entries with count `0` (no live reference) and deletes the matching files. `IncrementChunkRefs` / `DecrementChunkRefs` are taken under a single mutex; the bootstrap path reconstructs counts from metadata at startup if a `chunkref:` snapshot is missing.

## Authentication

- HTTP API: per-bucket tokens stored as argon2id(secret). Bearer header.
- S3 API: SigV4 over an `S3Key` (access key + raw secret). Permissions: `read`, `write`, `delete`, `admin`. Keys can be restricted to a list of buckets.

## What lives where

| Kind                    | Pebble key prefix             |
|-------------------------|-------------------------------|
| Bucket info             | `bucket:`                     |
| Object metadata         | `metadata:`                   |
| Object version metadata | `version:`                    |
| Chunk refcount          | `chunkref:`                   |
| Multipart header        | `mpu:`                        |
| Multipart part          | `mpupart:`                    |
| Bucket token            | `token:`                      |
| S3 access key           | `s3key:`                      |

Chunks themselves are on the filesystem at `data/chunks/<aa>/<bb>/<blake3-hex>`.
