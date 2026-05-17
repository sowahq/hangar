---
title: Cluster mode
description: Distributed Hangar — HRW chunk placement, Reed-Solomon EC, key-sharded metadata, WAL catchup, anti-entropy, zone awareness, secret rotation, optional dRPC TLS.
---

Hangar can run as a multi-node cluster where chunks and metadata distribute across peers. Cluster mode is opt-in and disabled by default. A single binary handles both modes; flipping `cluster.enabled = true` and adding a `[cluster]` section is the entire delta.

## TL;DR

- One binary, one TOML section, no external coordination service (no Raft, no etcd, no Zookeeper).
- Chunks placed via HRW (Rendezvous Hashing). Default `RF=2` synchronous fan-out; opt-in Reed-Solomon `k+m` erasure coding via `ec_data_shards` + `ec_parity_shards`.
- Zone-aware HRW: shards spread across distinct zones before filling the remainder by pure HRW. Tolerates a full-zone outage when `zones ≥ m+1`.
- Object metadata key-sharded by HRW; primary synchronous + secondary async fan-out + per-primary write-ahead log for catchup.
- System state (buckets, S3 keys, SSE keyring, configs, layout) replicated to all peers via a Pebble write hook; cold-start nodes pull a snapshot.
- Eager rebalancer fires on layout change; single-flighted; counter exposed at `/admin/cluster/status`.
- Anti-entropy worker reconciles local chunks against HRW expectations. Under EC, reconstructs missing shards from peers or from any `k` survivors.
- Deep-scrub admin op: re-encode each chunk's `k+m` shards, brute-force leave-one-out to find silent corruption, repair locally.
- Secret rotation via comma-separated `shared_secret_b64 = "<new>,<old>"`. Truncated SHA-256 fingerprints surfaced for audit.
- dRPC over TCP with HMAC-SHA256 handshake. Optional mutual TLS (`tls_cert / tls_key / tls_ca / tls_server_name`).
- Protocol version handshake refuses mixed-major peers.
- GC, scrub and lifecycle run on the lowest-id alive node only.

## What clustering gives you

- **Distributed chunks** — content-addressed chunks placed via HRW, replicated (RF=2) or erasure-coded (`k+m`).
- **Zone-aware spread** — first-pass placement picks the highest-ranked node from each distinct zone, so EC shards survive a whole-zone outage.
- **Sharded metadata** — per-object metadata sharded across peers, so list/get/put load spreads with traffic.
- **Cluster-wide dedup** — BLAKE3 content addressing means identical payloads dedup across the entire cluster (RF=2 mode).
- **Replicated system state** — buckets, S3 access keys, SSE keyring, encryption configs, object lock, lifecycle, CORS, tagging, website and logging configs replicate to all peers transparently.
- **WAL catchup** — secondaries that miss writes while down catch up automatically from the primary's WAL on reconnect.
- **Anti-entropy** — under RF=2, missing chunks pulled from owner peers; orphans pruned after verifying a peer holds the copy. Under EC, missing shards reconstructed from any `k` surviving shards.
- **Eager rebalancer** — layout change → immediate anti-entropy pass, single-flighted via `atomic.Bool`. Counter `eager_rebalances` exposed at `/admin/cluster/status`. Toggle via `Runtime.SetEagerRebalanceEnabled`.
- **Deep-scrub** — admin-triggered sweep that gathers each chunk's k+m shards, decodes, BLAKE3-checks against the base hash, brute-forces a leave-one-out reconstruction to locate the bad shard, re-encodes canonical, repairs.
- **Cold-start bootstrap** — a fresh node joining an existing cluster pulls the current replicated KV snapshot.
- **Leader-gated periodic work** — GC, scrub and lifecycle run on the lowest-id alive node only.
- **Idempotent refcount ops** — `op_id` deduplication makes Inc/Dec retry-safe.

## Architecture

### Membership

Layout-driven. Each node either bootstraps (no `seeds`) or joins via a seed. The seed accepts the join after handshake (HMAC-SHA256 with shared secret), bumps the layout version, signs it, and replication propagates it to every peer. On startup, a joining node retries `Join` against every seed with exponential backoff (500ms → 5s, up to 30 attempts) so it tolerates a seed that's still starting up. dRPC bidirectional heartbeat stream every `heartbeat_ms` (default 500ms) between every pair of layout members. Three missed ticks (default 1.5s) flip a peer to `down`. Reconnect uses exponential backoff (1s → 30s). Peer goroutines spawn/cancel automatically when the layout changes — no restart required.

The handshake includes a protocol version. Major-version mismatch is refused at dial.

| Status     | Meaning                                                                 |
|------------|-------------------------------------------------------------------------|
| `active`   | Currently sending heartbeats within the 3× window                       |
| `suspect`  | Reserved for future use                                                 |
| `down`     | Missed three heartbeats; not eligible for HRW until reconnect            |
| `draining` | Set via `hangar cluster node drain` — HRW skips writes, finishes reads   |
| `unknown`  | Listed in peers but never seen alive                                    |

### Zone-aware HRW placement

Each chunk's content-addressed BLAKE3 hash (RF=2) or the base hash (EC) is fed to HRW. Layout nodes carry a `zone` label. `TopNZoneAware(key, nodes, count)` first picks the highest-ranked node from each distinct zone, then fills remaining slots from the rest of the HRW ranking. When zones are empty the behaviour collapses to plain HRW (backward-compatible).

Concretely:

1. Rank all alive nodes by HRW score.
2. First pass: walk ranked list, take a node iff its zone is unseen, until `count` taken or one node per zone consumed.
3. Second pass: fill remaining slots from the same ranking, skipping nodes already chosen.

For EC `k+m` with `k+m ≤ zones`, this guarantees at least one shard in every zone. Losing a whole zone still leaves `(zones − 1) × shards_per_zone ≥ k` survivors.

### Erasure coding (Reed-Solomon `k+m`)

Opt-in via `ec_data_shards` and `ec_parity_shards`. Default `0 + 0` → RF=2 replication; no behaviour change for existing deployments.

Pipeline on write:

1. Chunk hashed (BLAKE3) and zstd-compressed/sealed as usual.
2. `ECEncoder` (`klauspost/reedsolomon`) writes an 8-byte length prefix, splits into `k+m` equal-size shards.
3. Shards keyed `<base-hash>_s<idx>` under the same `chunks/<aa>/<bb>/` directory.
4. Placement: shard `i` lands on `ChunkOwnersStable(hash)[i]`, where `ChunkOwnersStable` ranks against the **full** layout (not just alive nodes), so a down peer's shard slot stays positional.
5. Refcount mirrored to all `k+m` owners on the base hash. Reaching zero leaves shards unreferenced; the local GC walker (with suffix-strip in `GetChunkHashFromPath`) sweeps them.

Pipeline on read:

1. Compute `k+m` positional owners.
2. Collect any `k` shards (local first, RPC fallback). Down owner's slot stays `nil`.
3. Decode + reconstruct via Reed-Solomon. Strip the 8-byte length prefix. Return.

Tolerance: any `m` shards may be missing. With zone-aware HRW and `zones ≥ m+1`, a full-zone outage is recoverable.

EC requires stable membership. Changing `k`/`m` on existing data is not supported (would force re-encode). Pick once, deploy, leave.

### Refcount idempotency

Distributed refcount Inc/Dec carries an `op_id` (12-byte hex). The receiver writes a marker key `refop:<op_id>` after applying; subsequent Inc/Dec with the same `op_id` short-circuit. Markers are sweeped after a configurable retention via `PurgeOldRefOps`.

### Metadata sharding

Metadata is sharded by `bucket+key` via HRW. Top 2 nodes own a key: primary + secondary. Writes:

1. Primary: synchronous local Put, then WAL append.
2. Secondary fan-out: best-effort goroutine, fire-and-forget.

Reads hit owners in HRW order. If primary is down (or RPC times out), the read transparently falls through to the secondary; `NotFound` on one owner does not short-circuit — the next owner is tried. Strong consistency in the steady state; secondaries can briefly lag during async fan-out.

### Write-ahead log (WAL)

Each node maintains a per-node WAL of metadata ops it accepted as primary (`mwal:e:<seq>` keys in Pebble). On peer recovery (a peer transitions `down → active`), the local node calls `ReplicaCatchup(peer, last_seq)` on the recovered peer to pull anything it missed, then advances its per-peer cursor. Entries older than 24h are purged on each tick.

### Anti-entropy

Hourly per-node scan, also triggerable via `POST /admin/cluster/anti-entropy/run`. Logic differs by mode.

**RF=2 mode.** For each known chunk hash (union of local `chunkref:` keys and chunk hashes referenced by replicated `metadata:` entries):

- compute HRW owners.
- if self is an owner but chunk file is missing → pull from another owner via `GetChunk` stream.
- if self is not an owner but chunk file exists → delete locally, but **only after verifying at least one expected owner has the chunk** (via `HasChunk`).

**EC mode.** Per-shard logic on `<base-hash>_s<idx>` files:

- *Orphan* — shard local but owner != self → verify owner has its copy first. If owner missing it (layout change), push the shard to owner before deleting locally.
- *Direct pull* — shard owner is a peer and missing locally → pull from that peer via `GetChunk`.
- *Reconstruct* — unrepaired self-owner gap (no peer has the shard either) → gather `k` surviving shards across owners → `ECEncoder.Reconstruct` → write back.

Stats: `scanned`, `pulled`, `reconstructed`, `deleted`, `errors`. Exposed in the admin response and the `eager_rebalances` counter at `/admin/cluster/status`.

### Eager rebalancer

The layout callback (`Cluster.SetLayoutCallback`) fires `reconcilePeers` + `triggerEagerRebalance`. The eager pass is a single-flighted call to `RunAntiEntropy` guarded by an `atomic.Bool`; concurrent layout changes coalesce. A counter `rebalanceCount` is exposed at `/admin/cluster/status` as `eager_rebalances`. Disable in tests or for maintenance via `Runtime.SetEagerRebalanceEnabled(false)`.

### Deep-scrub

Admin-only sweep for silent shard corruption. `POST /admin/cluster/deep-scrub/run` requires EC enabled (`ec_data_shards > 0`).

For each refcounted base hash:

1. Gather all `k+m` shards (local first, RPC pull for peer-owned).
2. Decode → BLAKE3 of decoded payload → compare to base hash. Match → mark `verified`.
3. Mismatch → leave-one-out reconstruction: for each shard, drop it, try to decode + hash. The single shard whose absence yields the correct hash is the bad one.
4. Re-encode canonical shards. Replace the bad shard locally (uses `Delete` + `PutRaw` because `LocalChunkStore.PutRaw` short-circuits on existing files).

Stats returned: `scanned`, `verified`, `corrupt`, `repaired`, `skipped`, `errors`, `duration_ms`. Expensive (amplifies read traffic by `k+m`× per chunk); run off-peak.

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

### Pending chunk leases

Pebble-backed `pending:<hash>` keys with a 1h TTL replaced the in-memory map. Survives restart and provides a natural GC safety window for in-flight uploads.

### Layout

A signed, versioned, declarative description of cluster topology. Includes node IDs, addresses, zones, capacities, tags. HMAC-SHA256 signed with the cluster shared secret (or the previous secret during a rotation window). Stored at `cluster:layout:<v>` plus `cluster:layout:current` pointer in Pebble. Layout is replicated like other system state; on receive, the node hot-reloads. HRW weighting picks up `Capacity` automatically.

## Setup

### Bootstrap with `hangar cluster init`

The CLI mints a 32-byte secret and emits a ready-to-paste `[cluster]` TOML block:

```sh
hangar cluster init \
    --listen :7000 \
    --seed 10.0.0.1:7000 --seed 10.0.0.2:7000 \
    --node-id n1 --zone rack-a --capacity 10000000000000 \
    --out /etc/hangar/n1-cluster.toml
```

Flags:

| Flag         | Default      | Notes                                                    |
|--------------|--------------|----------------------------------------------------------|
| `--listen`   | `:7000`      | dRPC bind address                                        |
| `--seed`     |              | Seed `host:port`, repeatable. Omit on the very first node|
| `--node-id`  | hostname     | Override the auto-derived id                             |
| `--zone`     |              | Zone label (recommended)                                 |
| `--capacity` | 0            | Optional weight for HRW                                  |
| `--out`      | stdout       | Write to file (mode `0600`)                              |

Merge the emitted block into each node's `config.toml`. Reuse the **same** `shared_secret_b64` on every peer; vary `node_id`, `zone`, `listen` per node.

### First node (seed)

```toml
[cluster]
enabled            = true
listen             = "10.0.0.1:7000"
shared_secret_b64  = "<base64 32 bytes>"
zone               = "rack-a"
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
zone               = "rack-b"
```

On start, dials a seed, calls `Join`, gets the current layout (which includes itself after the seed bumps the version), and starts heartbeating.

### Enabling erasure coding

Once `k+m` nodes are joined and the layout is stable, add to every node's `config.toml`:

```toml
[cluster]
ec_data_shards   = 4
ec_parity_shards = 2
```

Rolling-restart. New writes encode into `k+m` shards immediately. **Existing chunks remain RF=2** — they were not encoded; AE will not auto-encode them. To re-encode legacy data, copy buckets through S3 to a path that triggers a fresh PUT.

Common shapes:

| `k+m` | Overhead | Tolerated losses | Min nodes | Min zones (full-zone tolerance) |
|-------|---------:|-----------------:|----------:|--------------------------------:|
| 2 + 2 |    2.00× |                2 |         4 |                               3 |
| 4 + 2 |    1.50× |                2 |         6 |                               3 |
| 4 + 3 |    1.75× |                3 |         7 |                               4 |
| 6 + 3 |    1.50× |                3 |         9 |                               4 |
| 8 + 4 |    1.50× |                4 |        12 |                               5 |

EC requires stable membership. Pick `k+m` once.

### Enabling TLS on dRPC

```toml
[cluster]
tls_cert        = "/etc/hangar/node.crt"
tls_key         = "/etc/hangar/node.key"
tls_ca          = "/etc/hangar/ca.crt"        # optional → mutual TLS
tls_server_name = "node-a.cluster"            # optional SNI
```

`BuildTLSConfigs` returns `(server, client) *tls.Config`. With a CA configured, both sides verify peer certs against it (mutual auth). The dRPC listener wraps with `tls.NewListener`; `Dial` wraps the TCP connection with `tls.Client` before dRPC handshake.

Do not mix plaintext and TLS within a single window — roll all nodes within the heartbeat-miss budget.

### Configuration reference

```toml
[cluster]
enabled                = false             # default off
node_id                = "n1"              # optional, defaults to hostname
listen                 = "10.0.0.1:7000"   # dRPC bind
shared_secret_b64      = "<base64 32 bytes>"     # comma-list of 1..2 entries for rotation
seeds                  = ["10.0.0.1:7000"]       # >= 1 reachable peer (omit on the very first node)
zone                   = "rack-a"                # advertised in layout; drives zone-aware HRW
capacity               = 1000                    # optional weight for HRW
tags                   = ["ssd"]                 # optional metadata in layout
heartbeat_ms           = 500
ec_data_shards         = 0                       # set > 0 to enable EC
ec_parity_shards       = 0
meta_shards            = 256                     # reserved
metadata_sync_quorum   = false                   # reserved
tls_cert               = ""                      # optional dRPC TLS
tls_key                = ""
tls_ca                 = ""                      # CA pin → mutual TLS
tls_server_name        = ""                      # optional SNI override
```

`seeds` are dialed once at startup to bootstrap. They are just addresses (`host:port`) — no IDs needed.

## Secret rotation

`shared_secret_b64` accepts a comma-separated list of up to two entries:

```toml
shared_secret_b64 = "<new>,<old>"
```

- First entry = primary (used to sign **and** verify).
- Second entry = previous (used only to verify, kept during the rotation window).

Verification path: `Cluster.VerifyHello` tries primary first; on `ErrAuthFailed` and a previous secret present, falls back to it. Layout HMAC verification follows the same pattern via `verifyLayout`. Signing always uses the primary.

### Rolling procedure

1. Generate new secret on every node's config: `shared_secret_b64 = "<new>,<old>"`.
2. Rolling-restart, one node at a time. Each node accepts both during the window.
3. After all nodes are on the 2-entry list, audit via `GET /admin/cluster/secret/status`:
   ```json
   {
     "primary_fingerprint": "ab12cd34ef567890",
     "has_previous": true,
     "previous_fingerprint": "9988aabbccddeef0"
   }
   ```
   Fingerprints are the first 8 bytes of SHA-256 of the secret, hex-encoded. Raw bytes never leave the server.
4. Drop the previous entry: `shared_secret_b64 = "<new>"`.
5. Rolling-restart again. Done.

No window where nodes can't authenticate each other.

## Admin HTTP API

```
GET     /admin/cluster/status              → cluster view + layout + eager_rebalances counter
GET     /admin/cluster/layout              → current signed layout
PUT     /admin/cluster/layout              → apply a new layout (version must increase)
DELETE  /admin/cluster/node/:id            → remove node from layout
POST    /admin/cluster/node/:id/drain      → mark node draining
POST    /admin/cluster/anti-entropy/run    → trigger anti-entropy scan immediately
POST    /admin/cluster/deep-scrub/run      → EC-only: silent-corruption sweep
GET     /admin/cluster/secret/status       → truncated fingerprints (primary, previous)
```

## CLI

```bash
hangar cluster init [flags]                    # generate secret + scaffold TOML block
hangar cluster status                          # cluster view: self, view_version, layout_version, nodes
hangar cluster node remove <id>                # drop node from layout
hangar cluster node drain <id>                 # mark node draining (HRW skip writes, finish reads)
```

All commands accept `--server <admin-url>` (default `http://localhost:8080`).

## Adding and removing nodes

### Add

Just start the new node with `seeds = [<any-existing-addr>]`. The seed's `Join` handler bumps the layout, the new version replicates via the Pebble write hook, every node hot-reloads. The eager rebalancer fires immediately on every existing node to redistribute chunks toward the new owner.

### Remove

```bash
hangar cluster node remove n3 --server http://any-running-node:8080
```

Bumps the layout, drops `n3`, replicates. `n3` itself can still be running; it is no longer included in HRW placement. Stop the `n3` process when ready.

### Drain (clean shutdown)

```bash
hangar cluster node drain n3 --server http://...
```

Marks `n3` as `draining` in the layout. HRW excludes draining nodes from placement (new writes never land there). In-flight reads continue. After traffic settles, run `remove`, then stop the process.

## Observability

When `metrics.enabled = true`:

| Metric                                  | Type  | Meaning                                         |
|-----------------------------------------|-------|-------------------------------------------------|
| `hangar_cluster_view_version`           | gauge | Monotonic membership view version               |
| `hangar_cluster_layout_version`         | gauge | Currently applied layout version                |
| `hangar_cluster_alive_peers`            | gauge | Count of peers in `active` status               |
| `hangar_cluster_total_peers`            | gauge | Total peers known (self included)               |
| `hangar_cluster_gc_leader`              | gauge | 1 if this node owns GC/scrub/lifecycle, else 0  |
| `hangar_cluster_ec_data_shards`         | gauge | Configured EC `k`                               |
| `hangar_cluster_ec_parity_shards`       | gauge | Configured EC `m`                               |

Sampled every 5s from the cluster runtime. The `eager_rebalances` counter currently lives only in the admin HTTP response (not a Prometheus gauge yet).

## Failure scenarios

| Scenario                              | Behaviour                                                                                                        |
|---------------------------------------|------------------------------------------------------------------------------------------------------------------|
| Peer crashes                          | Heartbeat misses → `down` in 3× `heartbeat_ms`. HRW skips it. Reads fall through to remaining owners.            |
| Peer recovers                         | Heartbeat → `active`. The recovering node receives a `BulkSyncKV` snapshot on its end. Other nodes catch it up on per-key WAL via `ReplicaCatchup`. Eager rebalancer also fires. |
| Peer joins fresh (empty DB)           | Same as recovery + the new node's bootstrap pull populates system state. AE redistributes chunks lazily; eager rebalancer kicks an immediate pass. |
| Layout change                         | New layout signed, applied on one node, replicated to peers, hot-reloaded. Eager rebalancer runs. |
| Write to single-alive cluster (RF=2)  | RF=2 fan-out succeeds with 1/2 stored. Anti-entropy backfills the missing replica once the peer returns.         |
| EC: up to `m` shards down             | Reads silently reconstruct from any `k` survivors. Writes still need `k+m` positional slots; missing slots are reconstructed on next AE pass. |
| EC: whole zone down (zones ≥ m+1)     | Survivors across other zones still total ≥ `k`. Reads succeed, writes succeed with degraded shards filled lazily.|
| GC leader dies                        | Lowest-id alive peer takes over on the next tick. No coordination.                                                |
| Concurrent layout apply               | The layout endpoint rejects stale versions. Last write with the highest version wins.                            |
| Mixed-major binaries                  | Dial refused at handshake with `ErrVersionMismatch`. Roll the whole fleet to the same major.                     |

## Interop harness

`tools/clusterinterop/` orchestrates real `hangar` binaries (no in-process mocks) and runs end-to-end scenarios:

```bash
cd tools/clusterinterop && go build -o /tmp/clusterinterop .
go build -o /tmp/hangar .

# all baseline scenarios, fully self-managed
HANGAR_BIN=/tmp/hangar /tmp/clusterinterop all

# specific scenario
/tmp/clusterinterop drain
/tmp/clusterinterop wal-catchup
/tmp/clusterinterop majority-kill

# tunable durations
LONGRUN_SECONDS=30 SUSTAINED_SECONDS=120 /tmp/clusterinterop all
```

Scenarios covered:

| Scenario          | Topology         | What it validates                                                                                       |
|-------------------|------------------|---------------------------------------------------------------------------------------------------------|
| `basic`           | 2 nodes RF=2     | PUT-A → GET-B, small object                                                                            |
| `concurrent`      | 2 nodes RF=2     | 50 parallel uploads against both nodes, all readable                                                    |
| `drain`           | 3 nodes RF=2     | Drained node receives zero chunks for new writes (HRW respects `draining`)                              |
| `add-remove`      | 3 → 4 → 3 nodes  | Add a node dynamically via seed, then remove it; existing data survives                                  |
| `seed-failover`   | 3 nodes          | Kill the primary seed, new node joins via the secondary seed                                            |
| `wrong-secret`    | 2 nodes          | Joining with the wrong shared secret is refused at handshake                                            |
| `anti-entropy`    | 3 nodes RF=2     | Kill a peer, write objects, restart, manually trigger AE → chunks back on peer                          |
| `wal-catchup`     | 3 nodes RF=2     | Kill a peer, write 3 objects on primary, restart → secondary catches up via WAL replay                  |
| `large-multipart` | 3 nodes RF=2     | 50 MB multipart upload split across nodes, full GET round-trip cross-node                               |
| `rolling-restart` | 3 nodes          | Stop+restart each of 3 nodes sequentially; all 12 seeded objects readable from every node              |
| `majority-kill`   | 3 nodes          | Kill 2 of 3 nodes; survivor still serves chunks for which it is an HRW owner                            |
| `long-run`        | 2 nodes          | 4 workers writing for N seconds across both nodes; require zero errors                                  |
| `sustained`       | 3 nodes          | 6 workers PUT+GET against 3 nodes for N seconds; error rate must stay below 1%                          |
| `soak`            | 3 nodes (or 4 EC)| SIGINT or `SOAK_HOURS`-bounded long load. `SOAK_EC=1` swaps RF=2 for EC=2+2. `SOAK_CHURN=1` adds random kill+restart every `SOAK_CHURN_PERIOD` seconds; error gate relaxes 1 %→5 % under churn. |
| `ec`              | 4 nodes EC=2+2   | Kill 2 owners → full reconstruct on a 5th read                                                          |
| `ec-ae`           | 4 nodes EC=2+2   | Wipe one node's chunks, restart, `POST /admin/cluster/anti-entropy/run` → reconstructed counter ≥ 1     |
| `zone-spread`     | 6 nodes 3 zones  | HRW first-pass spreads shards across zones; kill zone B → all 30 objects reconstructed from A+C         |
| `ec-4plus3`       | 7 nodes 3 zones  | EC=4+3, kill whole zone A (3 nodes), reads still succeed                                                |
| `ec-6plus3`       | 9 nodes 3 zones  | EC=6+3, kill whole zone C (3 nodes), reads still succeed                                                |

## Limitations & non-goals

- **EC requires stable membership.** Changing `k`/`m` on existing data is not supported. Pick once.
- **Re-encoding legacy RF=2 → EC.** Enabling EC after data already exists does **not** auto-encode legacy chunks. They keep working under RF=2 alongside new EC writes. To re-encode, force a fresh PUT (S3 copy through a new key, then delete the old key).
- **Eager rebalancer counter is admin-only.** Not yet surfaced as a Prometheus gauge.
- **Reads on metadata** can briefly observe a stale value after a primary commit but before the async secondary fan-out lands. Strong reads always hit the primary.
- **No multi-DC.** Single-zone cluster latency assumptions still apply (heartbeat = LAN-class). Zone-aware HRW gives multi-rack tolerance, not multi-DC.
- **No rolling-version upgrades.** All cluster nodes must run the same Hangar binary major version. Mixed-major is refused at handshake.
- **No cross-bucket transactions** — same as AWS S3.
- **List ops are eventually consistent across nodes** when secondaries are catching up.

## Tuning

| Parameter             | Default        | When to change                                                  |
|-----------------------|----------------|-----------------------------------------------------------------|
| `heartbeat_ms`        | 500            | Lower for faster failure detection (LAN); raise on noisy links. |
| Anti-entropy interval | 1h (hardcoded) | Lower on small clusters or frequent layout changes.             |
| WAL retention         | 24h            | Raise if peers can be offline for >24h. Below 24h risks gaps.   |
| Pending lease TTL     | 1h             | Window for in-flight uploads before GC reclaims orphans.        |
| Catchup loop interval | 15s            | Frequency of "peer just came back, pull from it" check.         |
| EC `k+m`              | 0 + 0 (RF=2)   | Set once at deploy. `2+2` small clusters, `4+2` / `6+3` larger.  |
| Eager rebalancer      | enabled        | Disable via `Runtime.SetEagerRebalanceEnabled(false)` for maintenance — AE will still catch up lazily. |
