---
title: Introduction
description: What Hangar is, who it's for, and the design choices behind it.
---

Hangar is a single-binary, self-hosted object storage server written in Go. It exposes both a small native HTTP API and an S3-compatible API on top of the same storage layer.

It runs as a single node today. Distribution, replication, and erasure coding are on the roadmap — not built yet. The current goal is to be something close to "S3 for your homelab or a single production node" without the operational complexity of MinIO/Ceph/Garage when you do not need their guarantees right now.

## When Hangar fits

- You need an S3 endpoint behind your apps (`aws s3`, `rclone`, `boto3`, `mc`, etc.) but you do not want to operate a cluster.
- You back up the underlying disk (or filesystem snapshot) anyway and can accept "single-node durability" — i.e. the durability of your disk — until distribution lands.
- You want content-addressed storage so duplicate uploads do not cost twice the bytes.
- You want server-side encryption at rest with either a server-held key (SSE-S3) or per-request client keys (SSE-C).

## When Hangar does not fit

- You need replication, erasure coding, or any kind of multi-node availability **today**. These are planned but not built yet — see [Roadmap](/roadmap/).
- You need SSE-KMS, bucket default encryption, or object lock / WORM. None of these exist yet — see [Limitations](/limitations/).
- You want a fully audited, formally certified S3 implementation. Hangar implements a working subset; see [S3 compatibility](/s3-compatibility/) for what is in and out.

## Design choices

### Content-addressed chunks

Every uploaded object is split into fixed-size chunks (default 4 MiB). Each chunk is hashed with [blake3](https://github.com/zeebo/blake3). The hash becomes the chunk's filename on disk under `data/chunks/<aa>/<bb>/<hex>`. If the same chunk already exists, the second upload writes nothing new — only the reference count is bumped.

This means:

- Two uploads of the same file consume the same disk as one upload.
- Two files that happen to share a 4 MiB region (think VM images, archives, repeated headers) share the underlying bytes for that region.
- Encrypted objects break **cross-object** dedup because the ciphertext depends on the key and nonce. Intra-object dedup still works.

### Pebble as the only KV

All metadata (buckets, objects, versions, multipart uploads, tokens, S3 keys, chunk reference counts, lifecycle/CORS configs, SSE keyring) lives in an embedded [Pebble](https://github.com/cockroachdb/pebble) LSM at `data/store/`. No external database, no external cache. That is also the main reason Hangar is single-writer per node: Pebble is.

### zstd + AEAD pipeline

Chunks are compressed with zstd (level `SpeedBetterCompression`) before being written. When SSE is in use, they are encrypted with AES-256-GCM after compression. Read path reverses the pipeline: open file → AEAD-open (if SSE) → zstd-decode → emit.

### Two APIs, same storage

- **Native HTTP** (`:8080` by default) — flat `/:bucket/:key`, JSON responses, Bearer tokens.
- **S3** (`:9000` by default, opt-in) — SigV4-authenticated, XML responses, path-style routing. Same buckets, same objects, same versions.

The split exists because the native API was first and is convenient for CLI / scripting; the S3 surface was added so unmodified AWS SDKs can talk to Hangar.

## Maturity

Current series: **v0.9.x** — feature-complete first cut, pre-1.0. The on-disk format may evolve. Backups are supported (`hangar backup create` + `restore`) but cross-version migrations are not guaranteed before 1.0 is tagged. The HTTP/S3 wire surface is stable for what is documented; anything not documented is not a stable contract. v1.0.0 lands once a real production deployment has lived on it long enough to commit to SemVer stability.
