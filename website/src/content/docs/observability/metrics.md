---
title: Metrics (Prometheus)
description: Opt-in Prometheus endpoint, exposed series, and what to alert on.
---

Hangar exposes Prometheus metrics on its own port when `[metrics] enabled = true`. Keeping it on a separate listener lets you firewall scrape traffic away from the public S3 / HTTP surface.

## Enable

```toml
[metrics]
enabled   = true
bind_addr = ":9100"
```

Scrape at `http://<host>:9100/metrics`. The exposition includes the standard `go_*` and `process_*` collectors plus the `hangar_*` series below.

## Series

### Requests

| Name                                 | Type      | Labels                | Notes |
|--------------------------------------|-----------|-----------------------|-------|
| `hangar_requests_total`              | counter   | `api`, `method`, `status` | `api` is `http` or `s3` |
| `hangar_request_duration_seconds`    | histogram | `api`, `method`, `status` | buckets: 1 ms … 30 s |
| `hangar_requests_inflight`           | gauge     | `api`                 |       |
| `hangar_multipart_inflight`          | gauge     | —                     | currently-open multipart uploads |

### Garbage collection

| Name                                 | Type    |
|--------------------------------------|---------|
| `hangar_gc_last_tick_seconds`        | gauge   |
| `hangar_gc_deleted_chunks_total`     | counter |
| `hangar_gc_freed_bytes_total`        | counter |
| `hangar_gc_orphan_chunks`            | gauge   |
| `hangar_gc_total_chunks`             | gauge   |

### Scrub

| Name                                  | Type    |
|---------------------------------------|---------|
| `hangar_scrub_last_tick_seconds`      | gauge   |
| `hangar_scrub_corrupted_total`        | counter |
| `hangar_scrub_quarantined_total`      | counter |
| `hangar_scrub_bytes_scanned_total`    | counter |
| `hangar_scrub_missing_files`          | gauge   |
| `hangar_scrub_dangling_refs`          | gauge   |

### Disk

| Name                              | Type  |
|-----------------------------------|-------|
| `hangar_disk_free_bytes`          | gauge |
| `hangar_disk_total_bytes`         | gauge |
| `hangar_disk_node_used_bytes`     | gauge |
| `hangar_disk_node_max_bytes`      | gauge |

## What to alert on

Sensible defaults:

- `hangar_scrub_corrupted_total` rate > 0 — bit rot or hardware fault. Page immediately.
- `hangar_disk_free_bytes` projected to hit `min_free_bytes` within an hour — disk safeguard about to kick in.
- `histogram_quantile(0.99, sum(rate(hangar_request_duration_seconds_bucket[5m])) by (le, api))` above your SLO.
- `hangar_requests_total{status=~"5.."}` rate > 0 sustained.
- `time() - hangar_gc_last_tick_seconds > 2 * interval_hours * 3600` — GC stuck.

## Notes

- The metrics endpoint is **unauthenticated**. Bind it to a private interface or restrict at the firewall / reverse proxy.
- Histograms cost some memory per label combination. The `api`/`method`/`status` cardinality is bounded (~10 × 7 × ~10), but if you front Hangar with a reverse proxy that turns 4xx into other codes, watch the series count.
- The disk sampler runs only when metrics are enabled. The disk safeguards themselves do their own `statfs` per PUT regardless.
