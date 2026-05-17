---
title: Cluster mode
description: Distributed Hangar with HRW chunk placement, sharded metadata, replicated KV.
---

Hangar can run as a multi-node cluster where chunks and metadata are distributed across peers. Cluster mode is opt-in and disabled by default.

## What clustering gives you

- **Distributed chunks** — content-addressed chunks placed via HRW (Rendezvous Hashing), replicated to peer nodes for redundancy.
- **Sharded metadata** — per-object metadata key-sharded across peers, so list/get/put load spreads with traffic.
- **Replicated system state** — buckets, S3 access keys, SSE keyring, encryption configs, object lock, lifecycle, CORS, tagging, website and logging configs replicate to all peers transparently.
- **Cold-start bootstrap** — a new node joining an existing cluster pulls the current replicated KV snapshot at startup.
- **Leader-gated periodic work** — GC, scrub and lifecycle run on the lowest-id alive node only, so background workers don't race.

The wire protocol is Storj dRPC over TCP. Peer connections are authenticated with a 32-byte shared secret and an HMAC-SHA256 handshake. There is no TLS layer; deploy behind a VPN or in a trusted subnet.

## Configuration

Add a `[cluster]` section to each node's `config.toml`:

```toml
[cluster]
enabled            = true
node_id            = "n1"
listen             = "10.0.0.1:7000"
shared_secret_b64  = "<base64 32 bytes — must match across all nodes>"
peers              = ["n2@10.0.0.2:7000", "n3@10.0.0.3:7000"]
heartbeat_ms       = 500
```

- `node_id` — short, stable, unique. Used by HRW.
- `listen` — local address for the dRPC server.
- `shared_secret_b64` — generate with `head -c 32 /dev/urandom | base64`. Same value on every node.
- `peers` — list of `<id>@<host:port>` entries identifying the other nodes.
- `heartbeat_ms` — heartbeat cadence. Three missed ticks (default 1.5 s) flip a peer to `down`.

Each node also needs the usual `data_directory`, `[api]`, `[s3]` (if you want S3) and `[storage]` sections. Nodes can be heterogeneous (different storage classes), but the cluster requires identical `shared_secret_b64`, compatible Hangar versions, and reachable peer addresses.

## Status

The cluster's view is exposed on the admin HTTP API:

```bash
curl -s http://10.0.0.1:8080/admin/cluster/status
```

```json
{
  "self": "n1",
  "view_version": 7,
  "layout_version": 0,
  "heartbeat_ms": 500,
  "nodes": [
    {"id":"n1","addr":"10.0.0.1:7000","status":"active","last_seen_ms":1779025401378,"generation":1},
    {"id":"n2","addr":"10.0.0.2:7000","status":"active","last_seen_ms":1779025401388,"generation":1},
    {"id":"n3","addr":"10.0.0.3:7000","status":"down",  "last_seen_ms":1779024401388,"generation":1}
  ]
}
```

`status` is one of `active`, `suspect`, `down`, `draining`, `unknown`.

## What replicates and how

| Class               | Mechanism                                        | Notes                                                                  |
|---------------------|--------------------------------------------------|------------------------------------------------------------------------|
| Chunks              | HRW → top-2 owners, synchronous fan-out          | Both owners must succeed for the write to return                       |
| Object metadata     | HRW shard by `bucket+key`, primary + 1 secondary | Primary write synchronous, secondary best-effort async                 |
| Buckets, S3 keys, SSE keyring, encryption, lifecycle, CORS, tagging, website, logging, object-lock | Pebble write hook → fan-out to all alive peers | Cold-start nodes pull a snapshot on join via `BulkSyncKV` RPC |
| Refcount (per chunk)| Routed to chunk owners                           | Local Pebble on each owner                                             |

## Failure behaviour

- A peer that misses three heartbeats flips to `down`. Active peers stop being eligible HRW owners.
- Reads fall back from primary to secondary on metadata; chunk reads try each owner in turn.
- When a `down` peer comes back, its first heartbeat marks it `active` again and a fresh `BulkSyncKV` pull catches it up on replicated KV state.
- GC, scrub and lifecycle pause on non-leaders; the lowest-id alive node owns the schedule.

## Cluster interop harness

`tools/clusterinterop/` runs a fixed sequence of cross-node S3 scenarios against two running nodes:

```bash
cd tools/clusterinterop && go build -o /tmp/clusterinterop .
S3_AK=<ak> S3_SK=<sk> S3_BUCKET=<bucket> \
S3_A=http://10.0.0.1:9000 S3_B=http://10.0.0.2:9000 \
/tmp/clusterinterop
```

Scenarios: small PUT on A → GET on B, medium PUT on B → GET on A, cross-node LIST, dedup, cross-node DELETE propagation, multipart upload on A → GET on B.

## Limitations

- Replication factor for chunks is fixed at 2; configurable EC `k+m` is on the roadmap.
- Reads on metadata can briefly observe a stale value after a primary write returns before the async secondary fan-out lands. Strong reads always hit the primary.
- No multi-DC awareness yet.
- No TLS on the dRPC layer — use a VPN/VPC.
- All cluster nodes must run the same Hangar version. Rolling upgrades are not supported.
- Cluster topology is static (TOML `peers`). Adding a node requires updating every node's config and restarting.

## Recipe: bring up a fresh two-node cluster

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
listen = "127.0.0.1:7091"
shared_secret_b64 = "$SECRET"
peers = ["n2@127.0.0.1:7092"]
heartbeat_ms = 200
EOF

# similar config for n2 with swapped ids/ports/peers

hangar server --config /etc/hangar/n1.toml &
hangar server --config /etc/hangar/n2.toml &

# create state on n1, observe replication on n2
hangar bucket  create   --server http://127.0.0.1:8091 mybucket
hangar s3keys  create   --server http://127.0.0.1:8091 --perm admin

curl http://127.0.0.1:8092/admin/buckets   # bucket mybucket present
curl http://127.0.0.1:8092/admin/s3keys    # key replicated
```
