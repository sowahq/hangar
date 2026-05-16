---
title: Audit log
description: JSONL audit trail of admin actions and significant system events.
---

Hangar can write an audit trail of every admin action (bucket creation, token issuance, S3 key creation, SSE key rotation, lifecycle runs, etc.) plus server start/stop events. It is **off by default**.

## Enable

```toml
[audit]
enabled         = true
path            = ""        # defaults to <data_directory>/audit.log
max_size_mb     = 100
max_backups     = 5
retention_days  = 30
```

The file is created with `0600`. On every write, Hangar `fsync`s. Rotation happens when the active file would exceed `max_size_mb`; rotated files are named `<path>.<unix-nano>`, kept up to `max_backups`, and pruned by `retention_days`.

## Event shape

One JSON object per line:

```json
{
  "ts": 1715817600123,
  "actor": "admin",
  "actor_type": "admin",
  "action": "sse.rotate",
  "target_type": "sse_key",
  "target": "k-3f2a-1715817600",
  "result": "success",
  "ip": "127.0.0.1",
  "user_agent": "curl/8.6.0",
  "request_id": "",
  "error": ""
}
```

- `ts` — Unix milliseconds.
- `actor_type` — one of `admin`, `s3key`, `cli`, `system`.
- `action` — dot-namespaced, e.g. `bucket.create`, `bucket.delete`, `token.create`, `token.revoke`, `s3key.create`, `s3key.delete`, `sse.rotate`, `sse.activate`, `lifecycle.run`, `server.start`, `server.stop`.
- `result` — `success` or `error`. On error, `error` carries the message.

## Tail

```sh
curl 'http://localhost:8080/admin/audit?limit=200'
```

Returns the last N events (max `1000`, default `100`). Disabled audit returns `503`.

The raw file is also fair game — JSONL plays well with `jq`, `lnav`, and friends:

```sh
jq -c 'select(.action == "sse.rotate")' data/audit.log
```

## What is not audited

- Object-level `PutObject` / `GetObject` requests. Use the [Prometheus metrics](/observability/metrics/) (`hangar_requests_total` by api/method/status) for traffic shape; the audit log is for governance events, not access logs.
- Anonymous traffic to public-read buckets.
