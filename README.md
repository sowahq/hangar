# Hangar

Self-hosted object storage in Go. Content-addressed chunks (blake3 + zstd) on Pebble, served via Fiber. Native HTTP API and S3-compatible API on the same storage.

> Full documentation: **[hangar.mth.lc](https://hangar.mth.lc)**

Single-node or cluster mode. Cluster mode (HRW chunk placement RF=2 or Reed-Solomon `k+m`, key-sharded metadata, replicated system state, WAL catchup, anti-entropy, seed-based dynamic membership, zone-aware placement, deep-scrub, secret rotation) is beta — see [Cluster mode](#cluster-mode) below.

## Quickstart

```sh
make build
./bin/hangar server -c config.toml
```

A default `config.toml` is generated on first start. The HTTP API binds to `:8080`; the S3 API is disabled by default — enable it in `[s3]`.

### Docker

Pre-built multi-arch image on GHCR:

```sh
docker run --rm -p 8080:8080 -v $(pwd)/data:/data ghcr.io/sowahq/hangar:latest
```

Or build locally:

```sh
make docker
docker run --rm -p 8080:8080 -v $(pwd)/data:/data hangar:dev
```

### Pre-built binaries

Linux / macOS / Windows binaries for tagged releases: [github.com/sowahq/hangar/releases](https://github.com/sowahq/hangar/releases).

## Cluster mode

Hangar runs as a peer-to-peer cluster of 3–15 nodes in a single DC. dRPC transport, HRW chunk placement, key-sharded metadata, anti-entropy, seed-based membership. Default `cluster.enabled = false` — single-node behavior is unchanged.

### Bootstrap

```sh
hangar cluster init \
    --listen :7000 \
    --seed 10.0.0.1:7000 --seed 10.0.0.2:7000 --seed 10.0.0.3:7000 \
    --node-id n1 --zone dc1-rack-a --capacity 10000000000000 \
    --out /etc/hangar/cluster.toml
```

`init` mints a 32-byte `shared_secret_b64` and emits a `[cluster]` TOML block. Copy the same `shared_secret_b64` onto every node; vary `node_id`, `zone`, and `listen` per node. Merge the block into each node's `config.toml`, then on every node:

```sh
hangar server -c /etc/hangar/config.toml
```

Once all nodes have completed the heartbeat handshake, assign the layout from any one of them:

```sh
hangar cluster status                           # observe peers
hangar cluster node drain <id>                  # drain a node (read-only)
hangar cluster node remove <id>                 # remove from layout
```

### Erasure coding

EC is opt-in. Default `ec_data_shards = 0`, `ec_parity_shards = 0` → RF=2 replication. Enable explicitly:

```toml
[cluster]
ec_data_shards   = 4
ec_parity_shards = 2
```

Each chunk is split into k data + m parity shards. Storage overhead `(k+m)/k`; tolerates `m` shard losses per chunk. Need ≥ k+m nodes, ideally across ≥ m+1 zones so a full-zone outage stays recoverable. Reads reconstruct silently from any k shards.

Common shapes: `2+2` (3 nodes, 2× overhead, tolerates 2), `4+2` (6 nodes, 1.5× overhead, tolerates 2), `6+3` (9 nodes, 1.5× overhead, tolerates 3 — e.g. an entire zone). Membership must be stable; changing `k`/`m` on existing data is not supported.

### Zones

Set `zone =` per node. HRW does a first pass that picks the highest-ranked node from each distinct zone before filling remaining slots by pure HRW — so EC shards spread across zones for multi-rack / multi-DC failure tolerance.

### Secret rotation

`shared_secret_b64` accepts a comma-separated list of up to two entries: first = primary (sign + verify), second = previous (verify only).

```toml
shared_secret_b64 = "<new>,<old>"
```

Procedure: edit every node's config to `"<new>,<old>"`, rolling-restart, verify `GET /admin/cluster/secret/status` (returns truncated SHA-256 fingerprints — never raw bytes), then drop the comma + old entry in a second rolling restart. No window where nodes can't talk.

### TLS rotation

Same model. dRPC TLS is opt-in via `tls_cert / tls_key / tls_ca / tls_server_name`. To rotate, issue new certs from the same CA, deploy them, rolling-restart. The CA pin survives the leaf rotation.

### Admin endpoints

| Method | Path | Purpose |
|---|---|---|
| GET  | `/admin/cluster/status`            | Layout, peers, leader, EC config, eager-rebalance count |
| GET  | `/admin/cluster/secret/status`     | Truncated secret fingerprints |
| POST | `/admin/cluster/anti-entropy/run`  | Trigger anti-entropy sweep |
| POST | `/admin/cluster/deep-scrub/run`    | Reconstruct + verify each chunk's k+m shards; repair silent corruption |

### Troubleshooting

| Symptom | Likely cause | Action |
|---|---|---|
| `ErrAuthFailed` on dial | Secret mismatch | Confirm same `shared_secret_b64` on all nodes; during rotation, ensure 2-entry list deployed |
| `version mismatch` | Mixed-major binaries | Roll all nodes to the same major; minor mismatch is permitted |
| Reads return 404 just after node loss | AE hasn't caught up | `POST /admin/cluster/anti-entropy/run`; check `reconstructed` counter |
| Steady-state shard count drift after node add | Eager rebalancer disabled | `GET /admin/cluster/status` → `eager_rebalances`; toggle via `Runtime.SetEagerRebalanceEnabled(true)` |
| Silent shard corruption suspected | Disk bitrot | `POST /admin/cluster/deep-scrub/run`, inspect `corrupt` / `repaired` stats |
| Node won't rejoin after restart | Stale layout (drained or removed) | `hangar cluster status`; re-add via layout if removed; un-drain otherwise |

Deeper design notes, migration playbook, and bench numbers live under `docs/cluster/` and on [hangar.mth.lc](https://hangar.mth.lc).

## Development

```sh
make test        # tests
make test-race   # race detector
make vet         # static analysis
make fmt         # format
```

## License

[AGPL-3.0](LICENSE)
