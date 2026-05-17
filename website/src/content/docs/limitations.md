---
title: Limitations
description: What Hangar does not do (yet). Read this before adopting.
---

Hangar is honest about scope. If any of the items below are blockers for your use case, pick a different system rather than fighting Hangar.

## Durability and availability

- **Cluster mode is beta.** Default deployment is single-node. Cluster mode ([see /cluster/](/cluster/)) is built and validated end-to-end via the `clusterinterop` harness (13 scenarios) but has not lived under sustained production load. Erasure coding `k+m` is not yet wired — cluster mode currently uses RF=2 replication for chunks, which means 2× storage versus 1× single-node. For single-node deployments, durability ceiling is the underlying disk — use RAID, ZFS, or filesystem snapshots, and back up.
- **Pebble is single-writer per node.** You cannot run two `hangar server` processes against the same `data_directory`. Pebble holds an exclusive lock and a second process will fail to start. Cluster mode does not change this for the local store — distribution sits above the per-node engine.
- **Cluster reads are strongly consistent on primary, eventually consistent on secondary fan-out.** A read that hits the primary always sees the latest write. A read that falls through to a secondary may briefly see the previous version during async fan-out. WAL catchup on peer recovery is best-effort within the 24 h retention window.
- **No multi-DC.** Single zone only. Cluster heartbeat assumes LAN-class latencies.
- **No rolling-version upgrades.** All cluster nodes must run the same Hangar binary. Stop everywhere, upgrade, restart.

## Backups

- `hangar backup create` requires the server to be **stopped** (Pebble lock). It writes a consistent snapshot via `pebble.Checkpoint` + hard-link (or copy) of `data/chunks/`. Restore is symmetric and requires an empty destination.
- There is no online / hot backup, no incremental backup, and no PITR. If you need those, snapshot the filesystem under the running server (ZFS / LVM / btrfs) — that is supported by Pebble's WAL, but you take responsibility for the snapshot's atomicity.

## Encryption

- **SSE-S3** uses one server master key configured via `[security] master_key_b64` or `HANGAR_MASTER_KEY`. The keyring supports rotation (`POST /admin/sse/keys/rotate`) but **old objects are not re-keyed**. They keep working under their original key ID, which is recorded in object metadata. Losing an old key id means losing the objects it sealed.
- **No SSE-KMS.** There is no KMS provider integration.
- **No bucket default encryption.** `PUT /:bucket?encryption` is not implemented. Clients must send the `x-amz-server-side-encryption` header on each PUT.
- **Cross-object dedup is broken for encrypted chunks.** Different key or nonce → different ciphertext → different blake3 → different chunk file. Intra-object dedup (same chunk repeated inside one object) still works.
- **CopyObject across SSE keys cannot reuse chunks.** A copy that changes the SSE key (or moves between SSE and plaintext) is implemented as full decrypt + re-encrypt. Same-key copies stay cheap.

## S3 surface

See [S3 compatibility](/s3-compatibility/) for the full matrix. The main gaps:

- **No virtual-host-style addressing.** Path-style only (`http://host:9000/bucket/key`). Configure your SDK with `UsePathStyle: true`.
- **No ACLs, no bucket policies.** Permissions are coarse: per-token (`read` / `write` / `delete` / `admin`) and optional per-bucket scoping on S3 access keys.
- **No object tagging, no object metadata beyond standard headers** (Content-Type, user-defined headers are not persisted today).
- **No Object Lock / WORM, no Legal Hold.**
- **No replication APIs** (`PutBucketReplication`, etc.). Hangar replicates internally in cluster mode but does not expose S3 replication configuration to clients.
- **No `PutBucketNotificationConfiguration`, no event streams.** Webhooks / SNS / SQS are out of scope.
- **No `SelectObjectContent`, no Glacier tiers.**

## API surface (native)

- **Admin endpoints are unauthenticated.** They live under `/admin/*` on the HTTP API port. Bind that port to localhost or put a reverse proxy with auth in front. The S3 port can be exposed publicly; it requires SigV4-signed requests.
- **No multi-tenant model.** A token grants access to a bucket. There is no organization / project / IAM hierarchy.

## Operational

- **No Prometheus by default.** Metrics are opt-in via `[metrics] enabled = true` on a separate port. See [Metrics](/observability/metrics/).
- **Audit log is opt-in.** Same story — `[audit] enabled = true`. JSONL with size + age rotation. See [Audit](/operations/audit/).
- **No web UI.** Admin is HTTP API + the bundled `hangar` CLI. No dashboard.
- **No migration helpers across versions.** The on-disk format is stable enough for routine restarts but **pre-1.0 means cross-version upgrades may require a backup + restore cycle**. Read the release notes.
- **No TLS on the cluster dRPC layer.** Cluster traffic is authenticated by HMAC-SHA256 with a 32-byte shared secret but not encrypted. Deploy the cluster behind a VPN / WireGuard / VPC.

## Performance caveats

- Metadata writes default to `pebble.Sync` (one fsync per write) — durable but slow on spinning disks. Hangar is built for SSDs. Set `[storage] sync_writes = false` to use `pebble.NoSync` and trade durability for throughput; recent writes may be lost on power loss or hard kill.
- The chunker reuses a chunk-sized buffer via `sync.Pool`. Peak memory is still bounded by `chunk_size` × concurrent uploads, but allocations are amortized across requests. The default `chunk_size = 4 MiB` is intentionally modest.
- `ListObjectsV2` paginates via Pebble's iterator. Listing a bucket with millions of objects is bounded by the page size but the underlying scan is linear in items returned.

## Compliance

Hangar is not certified against SOC 2, ISO 27001, PCI-DSS, HIPAA, or anything else. Treat it as commodity infrastructure you operate yourself.
