---
title: Cluster mode
description: Distributed Hangar with HRW chunk placement, sharded metadata, WAL catchup, anti-entropy.
---

Hangar can run as a multi-node cluster where chunks and metadata are distributed across peers. Cluster mode is opt-in and disabled by default. A single binary handles both modes; flipping `cluster.enabled = true` and adding a `[cluster]` section is the entire delta.

## TL;DR

- One binary, one TOML section, no external coordination service (no Raft, no etcd, no Zookeeper).
- Chunks placed via HRW (Rendezvous Hashing) with RF=2 synchronous fan-out.
- Object metadata key-sharded by HRW; primary synchronous + secondary async fan-out + per-primary write-ahead log for catchup.
- System state (buckets, S3 keys, SSE keyring, configs, layout) replicated to all peers via a Pebble write hook; cold-start nodes pull a snapshot.
- GC, scrub and lifecycle run on the lowest-id alive node only.
- Anti-entropy worker reconciles local chunks against HRW expectations.
- dRPC over TCP, HMAC-SHA256 handshake, 32-byte shared secret. No TLS — deploy behind a VPN.

## What clustering gives you

- **Distributed chunks** — content-addressed chunks placed via HRW, replicated to peer owners.
- **Sharded metadata** — per-object metadata sharded across peers, so list/get/put load spreads with traffic.
- **Cluster-wide dedup** — BLAKE3 content addressing means identical payloads dedup across the entire cluster.
- **Replicated system state** — buckets, S3 access keys, SSE keyring, encryption configs, object lock, lifecycle, CORS, tagging, website and logging configs replicate to all peers transparently.
- **WAL catchup** — secondaries that miss writes while down catch up automatically from the primary's WAL on reconnect.
- **Anti-entropy** — chunks pulled from owner peers if missing locally; orphans pruned after verifying a peer holds the copy.
- **Cold-start bootstrap** — a fresh node joining an existing cluster pulls the current replicated KV snapshot.
- **Leader-gated periodic work** — GC, scrub and lifecycle run on the lowest-id alive node only.
- **Idempotent refcount ops** — `op_id` deduplication makes Inc/Dec retry-safe.

## Architecture

### Membership

Static peer list in each node's TOML. dRPC bidirectional heartbeat stream every `heartbeat_ms` (default 500ms). Three missed ticks (default 1.5s) flip a peer to `down`. Reconnect uses exponential backoff (1s → 30s).

| Status     | Meaning                                                                 |
|------------|-------------------------------------------------------------------------|
| `active`   | Currently sending heartbeats within the 3× window                       |
| `suspect`  | Reserved for future use                                                 |
| `down`     | Missed three heartbeats; not eligible for HRW until reconnect            |
| `draining` | Manually flagged (reserved; CLI surface coming)                          |
| `unknown`  | Listed in peers but never seen alive                                    |

### Chunk placement

Each chunk's content-addressed BLAKE3 hash is fed to HRW. Top `RF` candidates among alive layout nodes are selected. Default `RF = 2` (configurable EC `k+m` is on the roadmap). Write fans out synchronously to all owners — the write returns only when at least one owner stored. Reads try each owner in HRW order; first hit wins; `pebble.ErrNotFound` short-circuits.

### Metadata sharding

Metadata is sharded by `bucket+key` via HRW. Top 2 nodes own a key: primary + secondary. Writes:

1. Primary: synchronous local Put, then WAL append.
2. Secondary fan-out: best-effort goroutine, fire-and-forget.

Reads hit owners in HRW order. If primary is down, secondary serves the read. Strong consistency in the steady state; secondaries can briefly lag during async fan-out.

### Write-ahead log (WAL)

Each node maintains a per-node WAL of metadata ops it accepted as primary (`mwal:e:<seq>` keys in Pebble). On peer recovery (a peer transitions `down → active`), the local node calls `ReplicaCatchup(peer, last_seq)` on the recovered peer to pull anything it missed, then advances its per-peer cursor. Entries older than 24h are purged on each tick.

### Anti-entropy

Hourly per-node scan: for each `chunkref:<hash>` key,

- compute HRW owners
- if self is an owner but chunk file is missing → pull from another owner via `GetChunk` stream
- if self is not an owner but chunk file exists → delete locally, but **only after verifying at least one expected owner has the chunk** (via `HasChunk`)

This converges placement after layout changes, node loss, or write failures.

### Replicated KV

A pebble write hook fires on every Put/Delete. Keys matching a whitelist of system prefixes are fanned out to all alive peers via `ReplicateKV` RPC. Receiving nodes apply the write via `PutSilent`/`DeleteSilent` (which bypass the hook to avoid loops). Replicated prefixes:

```
s3key:  bucket:  token:  encryption:  objectlock:
ssekr:  lifecycle:  cors:  tagging:  website:  logging:
cluster:layout:
```

On startup, the node calls `BulkSyncKV(prefixes)` on the first alive peer to pull a full snapshot — this is how a cold-start node joins an existing cluster without manual intervention.

### Leader gating

`Cluster.IsGCLeader()` returns true iff this node is the lowest-id `Active` peer. GC, scrub and lifecycle schedulers skip their tick body when not the leader. Switches automatically when the current leader goes down.

### Refcount idempotency

Distributed refcount Inc/Dec carries an `op_id` (12-byte hex). The receiver writes a marker key `refop:<op_id>` after applying; subsequent Inc/Dec with the same `op_id` short-circuit. Markers are sweeped after a configurable retention via `PurgeOldRefOps`.

### Pending chunk leases

Pebble-backed `pending:<hash>` keys with a 1h TTL replaced the in-memory map. Survives restart and provides a natural GC safety window for in-flight uploads.

### Layout

A signed, versioned, declarative description of cluster topology. Includes node IDs, addresses, zones, capacities, tags. HMAC-SHA256 signed with the cluster shared secret. Stored at `cluster:layout:<v>` plus `cluster:layout:current` pointer in Pebble. Layout is replicated like other system state; on receive, the node hot-reloads. HRW weighting picks up `Capacity` automatically.

## Setup — two-node cluster

```bash
SECRET=$(head -c 32 /dev/urandom | base64)

cat > /etc/hangar/n1.toml <<EOF
data_directory = "/var/lib/hangar/n1"
[api]
bind_addr = ":8091"
[s3]
enabled = true
bind_addr = ":9101"
region = "us-east-1"
[storage]
chunk_size = 4194304
[garbage_collection]
interval_hours = 24
[cluster]
enabled = true
node_id = "n1"
listen = "10.0.0.1:7000"
shared_secret_b64 = "$SECRET"
peers = ["n2@10.0.0.2:7000"]
heartbeat_ms = 500
EOF

# same for n2, with swapped node_id / listen / peers / ports

hangar server --config /etc/hangar/n1.toml &
hangar server --config /etc/hangar/n2.toml &

# create state on n1; n2 picks it up
hangar bucket create  --server http://10.0.0.1:8091 mybucket
hangar s3keys create  --server http://10.0.0.1:8091 --perm admin

# query state
hangar cluster status --server http://10.0.0.1:8091
hangar cluster status --server http://10.0.0.2:8091
```

## Configuration reference

```toml
[cluster]
enabled                = false             # default off
node_id                = "n1"              # unique, stable
listen                 = "10.0.0.1:7000"   # dRPC bind
shared_secret_b64      = "<base64 32 bytes>"
peers                  = ["n2@10.0.0.2:7000", "n3@10.0.0.3:7000"]
heartbeat_ms           = 500
ec_data_shards         = 4                  # reserved (not yet wired)
ec_parity_shards       = 2                  # reserved
meta_shards            = 256                # reserved
metadata_sync_quorum   = false              # reserved
```

## CLI

```bash
hangar cluster status                        # JSON view: self, view_version, layout_version, nodes
hangar cluster layout show                   # current applied layout
hangar cluster layout apply <path.json>      # bump layout version
```

Layout JSON:

```json
{
  "version": 2,
  "nodes": [
    {"id": "n1", "addr": "10.0.0.1:7000", "capacity": 1000, "zone": "rack-a"},
    {"id": "n2", "addr": "10.0.0.2:7000", "capacity": 1000, "zone": "rack-b"},
    {"id": "n3", "addr": "10.0.0.3:7000", "capacity":  500, "zone": "rack-a"}
  ]
}
```

`version` must be strictly greater than the currently applied version. The server signs the layout with the cluster shared secret on apply.

## Admin HTTP API

```
GET  /admin/cluster/status     → cluster view + layout version
GET  /admin/cluster/layout     → current signed layout
PUT  /admin/cluster/layout     → apply a new layout (version must increase)
```

## Observability

When `metrics.enabled = true`:

| Metric                                  | Type  | Meaning                                         |
|-----------------------------------------|-------|-------------------------------------------------|
| `hangar_cluster_view_version`           | gauge | Monotonic membership view version               |
| `hangar_cluster_layout_version`         | gauge | Currently applied layout version                |
| `hangar_cluster_alive_peers`            | gauge | Count of peers in `active` status               |
| `hangar_cluster_total_peers`            | gauge | Total peers known (self included)               |
| `hangar_cluster_gc_leader`              | gauge | 1 if this node owns GC/scrub/lifecycle, else 0  |
| `hangar_cluster_ec_data_shards`         | gauge | Configured EC k (reserved)                      |
| `hangar_cluster_ec_parity_shards`       | gauge | Configured EC m (reserved)                      |

Sampled every 5s from the cluster runtime.

## Failure scenarios

| Scenario                              | Behaviour                                                                                                        |
|---------------------------------------|------------------------------------------------------------------------------------------------------------------|
| Peer crashes                          | Heartbeat misses → `down` in 3× `heartbeat_ms`. HRW skips it. Reads fall through to remaining owners.            |
| Peer recovers                         | Heartbeat → `active`. The recovering node receives a `BulkSyncKV` snapshot on its end. Other nodes catch it up on per-key WAL via `ReplicaCatchup`. |
| Peer joins fresh (empty DB)           | Same as recovery + the new node's bootstrap pull populates system state.                                          |
| Layout change                         | New layout signed, applied on one node, replicated to peers, hot-reloaded. Anti-entropy worker rebalances chunks. |
| Write to single-alive cluster          | RF=2 fan-out succeeds with 1/2 stored. Anti-entropy backfills the missing replica once the peer returns.         |
| GC leader dies                        | Lowest-id alive peer takes over on the next tick. No coordination.                                                |
| Concurrent layout apply               | The layout endpoint rejects stale versions. Last write with the highest version wins.                            |

## Interop harness

`tools/clusterinterop/` runs cross-node S3 scenarios against a live two-node cluster:

```bash
cd tools/clusterinterop && go build -o /tmp/clusterinterop .

# normal scenarios — requires the cluster already running
S3_AK=<ak> S3_SK=<sk> S3_BUCKET=<bucket> \
S3_A=http://127.0.0.1:9101 S3_B=http://127.0.0.1:9102 \
/tmp/clusterinterop

# WAL catchup — orchestrates the whole scenario incl. process kill/restart
HANGAR_BIN=/tmp/hangar CLUSTER_SECRET=$(head -c 32 /dev/urandom | base64) \
/tmp/clusterinterop wal-catchup
```

Scenarios covered: PUT-A→GET-B small, PUT-B→GET-A medium, cross-node LIST, dedup, cross-node DELETE propagation, multipart upload on A → GET on B, kill-B-PUT-on-A-restart-B-verify-via-WAL.

## Limitations & non-goals

- **Replication factor** is fixed at 2 for chunks. Configurable EC `k+m` is reserved in config but not yet wired into the placement path. Storage parity with MinIO comes when EC lands.
- **Reads on metadata** can briefly observe a stale value after a primary commit but before the async secondary fan-out lands. Strong reads always hit the primary.
- **No multi-DC** awareness. Single-zone cluster only. Latency-sensitive WAN deployment is explicitly out of scope.
- **No TLS** on the dRPC layer — assume VPN/WireGuard/VPC.
- **Same version everywhere** — rolling upgrades not supported. Stop, upgrade, restart.
- **Static topology** — adding/removing a node requires editing the peers list on every node and restarting. The layout CLI changes weighting and zone metadata but not membership.
- **No cross-bucket transactions** — same as AWS S3.
- **List ops are eventually consistent across nodes** when secondaries are catching up.
- **No EC-aware repair** yet — anti-entropy currently expects whole chunks, not parity shards.

## Tuning

| Parameter             | Default        | When to change                                                  |
|-----------------------|----------------|-----------------------------------------------------------------|
| `heartbeat_ms`        | 500            | Lower for faster failure detection (LAN); raise on noisy links. |
| Anti-entropy interval | 1h (hardcoded) | Lower on small clusters or frequent layout changes.             |
| WAL retention         | 24h            | Raise if peers can be offline for >24h. Below 24h risks gaps.   |
| Pending lease TTL     | 1h             | Window for in-flight uploads before GC reclaims orphans.        |
| Catchup loop interval | 15s            | Frequency of "peer just came back, pull from it" check.         |
