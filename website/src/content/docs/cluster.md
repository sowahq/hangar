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

Layout-driven. Each node either bootstraps (no `seeds`) or joins via a seed. The seed accepts the join after handshake (HMAC-SHA256 with shared secret), bumps the layout version, signs it, and replication propagates it to every peer. On startup, a joining node retries `Join` against every seed with exponential backoff (500ms → 5s, up to 30 attempts) so it tolerates a seed that's still starting up. dRPC bidirectional heartbeat stream every `heartbeat_ms` (default 500ms) between every pair of layout members. Three missed ticks (default 1.5s) flip a peer to `down`. Reconnect uses exponential backoff (1s → 30s). Peer goroutines spawn/cancel automatically when the layout changes — no restart required.

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

Reads hit owners in HRW order. If primary is down (or RPC times out), the read transparently falls through to the secondary; `NotFound` on one owner does not short-circuit — the next owner is tried. Strong consistency in the steady state; secondaries can briefly lag during async fan-out.

### Write-ahead log (WAL)

Each node maintains a per-node WAL of metadata ops it accepted as primary (`mwal:e:<seq>` keys in Pebble). On peer recovery (a peer transitions `down → active`), the local node calls `ReplicaCatchup(peer, last_seq)` on the recovered peer to pull anything it missed, then advances its per-peer cursor. Entries older than 24h are purged on each tick.

### Anti-entropy

Hourly per-node scan, also triggerable manually via `POST /admin/cluster/anti-entropy/run`. For each known chunk hash (derived by union of local `chunkref:` keys and chunk hashes referenced by replicated `metadata:` entries):

- compute HRW owners
- if self is an owner but chunk file is missing → pull from another owner via `GetChunk` stream
- if self is not an owner but chunk file exists → delete locally, but **only after verifying at least one expected owner has the chunk** (via `HasChunk`)

This converges placement after layout changes, node loss, or write failures. Scanning replicated metadata ensures a freshly-recovered peer (whose own refcount may not yet be up-to-date) still discovers what chunks it should hold.

### Replicated KV

A pebble write hook fires on every Put/Delete. Keys matching a whitelist of system prefixes are fanned out to all alive peers via `ReplicateKV` RPC. Receiving nodes apply the write via `PutSilent`/`DeleteSilent` (which bypass the hook to avoid loops). Replicated prefixes:

```
s3key:  bucket:  token:  encryption:  objectlock:
ssekr:  lifecycle:  cors:  tagging:  website:  logging:
cluster:layout:  mpu:  mpupart:  version:
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

## Setup

### First node (seed)

```toml
[cluster]
enabled            = true
listen             = "10.0.0.1:7000"
shared_secret_b64  = "<base64 32 bytes>"
# node_id defaults to hostname
# no seeds — this node bootstraps the cluster
```

On first start, this node auto-applies layout v1 = `{self}`.

### Any additional node

```toml
[cluster]
enabled            = true
listen             = "10.0.0.2:7000"
shared_secret_b64  = "<base64 32 bytes>"   # same secret as first node
seeds              = ["10.0.0.1:7000"]      # one or more reachable nodes
```

On start, dials a seed, calls `Join`, gets the current layout (which includes itself after the seed bumps the version), and starts heartbeating. Zero coordination from the operator — the binary self-registers.

### Full example: bring up a fresh 3-node cluster

```bash
SECRET=$(head -c 32 /dev/urandom | base64)

# node 1 — seed
cat > /etc/hangar/n1.toml <<EOF
data_directory = "/var/lib/hangar/n1"
[api]
bind_addr = ":8091"
[s3]
enabled = true
bind_addr = ":9101"
[storage]
chunk_size = 4194304
[garbage_collection]
interval_hours = 24
[cluster]
enabled = true
listen = "10.0.0.1:7000"
shared_secret_b64 = "$SECRET"
EOF

# node 2 / node 3 — seeds = [n1]
cat > /etc/hangar/n2.toml <<EOF
data_directory = "/var/lib/hangar/n2"
[api]
bind_addr = ":8091"
[s3]
enabled = true
bind_addr = ":9101"
[storage]
chunk_size = 4194304
[garbage_collection]
interval_hours = 24
[cluster]
enabled = true
listen = "10.0.0.2:7000"
shared_secret_b64 = "$SECRET"
seeds = ["10.0.0.1:7000"]
EOF

# launch — order matters for node 1 only on first ever start
ssh n1 hangar server --config /etc/hangar/n1.toml &
ssh n2 hangar server --config /etc/hangar/n2.toml &
ssh n3 hangar server --config /etc/hangar/n3.toml &

# verify on any node
hangar cluster status --server http://10.0.0.1:8091
hangar cluster layout show --server http://10.0.0.2:8091
```

### Configuration reference

```toml
[cluster]
enabled                = false             # default off
node_id                = "n1"              # optional, defaults to hostname
listen                 = "10.0.0.1:7000"   # dRPC bind
shared_secret_b64      = "<base64 32 bytes>"
seeds                  = ["10.0.0.1:7000"] # >= 1 reachable peer (omit on the very first node)
zone                   = "rack-a"           # optional, advertised in layout
capacity               = 1000               # optional weight for HRW
tags                   = ["ssd"]            # optional metadata in layout
heartbeat_ms           = 500
ec_data_shards         = 4                  # reserved (EC not yet wired)
ec_parity_shards       = 2                  # reserved
meta_shards            = 256                # reserved
metadata_sync_quorum   = false              # reserved
```

`seeds` are dialed once at startup to bootstrap. They are just addresses (`host:port`) — no IDs needed. Multiple seeds = tries each until one accepts.

## Adding and removing nodes

### Add

Just start the new node with `seeds = [<any-existing-addr>]`. The seed's `Join` handler bumps the layout, the new version replicates via the Pebble write hook, every node hot-reloads.

### Remove

```bash
hangar cluster node remove n3 --server http://any-running-node:8091
```

Bumps the layout, drops `n3`, replicates. `n3` itself can still be running; it is no longer included in HRW placement. Stop the `n3` process when ready.

### Drain (clean shutdown)

```bash
hangar cluster node drain n3 --server http://...
```

Marks `n3` as `draining` in the layout. HRW excludes draining nodes from placement (new writes never land there). In-flight reads continue. After traffic settles, run `remove`, then stop the process.

## CLI

```bash
hangar cluster status                          # cluster view: self, view_version, layout_version, nodes
hangar cluster layout show                     # current applied layout (signed)
hangar cluster layout apply <path.json>        # bump layout version (low-level)
hangar cluster node remove <id>                # drop node from layout
hangar cluster node drain <id>                 # mark node draining (HRW skip writes)
```

All commands accept `--server <admin-url>` (default `http://localhost:8080`). The `--server` flag must come before positional arguments.

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
GET     /admin/cluster/status              → cluster view + layout version
GET     /admin/cluster/layout              → current signed layout
PUT     /admin/cluster/layout              → apply a new layout (version must increase)
DELETE  /admin/cluster/node/:id            → remove node from layout
POST    /admin/cluster/node/:id/drain      → mark node draining
POST    /admin/cluster/anti-entropy/run    → trigger anti-entropy scan immediately
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

`tools/clusterinterop/` orchestrates real `hangar` binaries (no in-process mocks) and runs end-to-end scenarios:

```bash
cd tools/clusterinterop && go build -o /tmp/clusterinterop .
go build -o /tmp/hangar .

# all scenarios, fully self-managed (spawns/kills processes, generates secret)
HANGAR_BIN=/tmp/hangar /tmp/clusterinterop all

# a single scenario
/tmp/clusterinterop drain
/tmp/clusterinterop wal-catchup
/tmp/clusterinterop majority-kill

# tunable durations
LONGRUN_SECONDS=30 SUSTAINED_SECONDS=120 /tmp/clusterinterop all
```

Scenarios covered:

| Scenario          | What it validates                                                                                  |
|-------------------|----------------------------------------------------------------------------------------------------|
| `basic`           | 2 nodes, PUT-A → GET-B, small object                                                              |
| `concurrent`      | 2 clients, 50 parallel uploads against both nodes, all readable                                    |
| `drain`           | Drained node receives zero chunks for new writes (HRW respects `draining`)                        |
| `add-remove`      | Add a node dynamically via seed, then remove it; existing data survives                            |
| `seed-failover`   | Kill the primary seed, new node joins via the secondary seed                                       |
| `wrong-secret`    | Joining with the wrong shared secret is refused at handshake                                       |
| `anti-entropy`    | Kill a peer, write objects, restart it, manually trigger anti-entropy → chunks back on peer        |
| `wal-catchup`     | Kill a peer, write 3 objects on primary, restart peer → secondary catches up via WAL replay        |
| `large-multipart` | 50 MB multipart upload split across both nodes, full GET round-trip cross-node                     |
| `rolling-restart` | Stop+restart each of 3 nodes sequentially; all 12 seeded objects readable from every node          |
| `majority-kill`   | Kill 2 of 3 nodes; the survivor still serves chunks for which it is an HRW owner (zero corruption) |
| `long-run`        | 4 workers writing for N seconds across both nodes; require zero errors                              |
| `sustained`       | 6 workers PUT+GET against 3 nodes for N seconds; error rate must stay below 1%                     |

## Limitations & non-goals

- **Replication factor** is fixed at 2 for chunks. Configurable EC `k+m` is reserved in config but not yet wired into the placement path. Storage parity with MinIO comes when EC lands.
- **Reads on metadata** can briefly observe a stale value after a primary commit but before the async secondary fan-out lands. Strong reads always hit the primary.
- **No multi-DC** awareness. Single-zone cluster only. Latency-sensitive WAN deployment is explicitly out of scope.
- **No TLS** on the dRPC layer — assume VPN/WireGuard/VPC.
- **Same version everywhere** — rolling upgrades not supported. Stop, upgrade, restart.
- **Mixed versions** — all cluster nodes must run the same Hangar binary version. Stop, upgrade everywhere, restart.
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
