---
title: Disk safeguards
description: Refuse writes before the data filesystem fills up.
---

A full filesystem under Pebble is a fast way to corrupt the store. Hangar can refuse `PutObject` (and `UploadPart`) once configurable thresholds are about to be crossed.

## Configure

```toml
[storage]
min_free_bytes  = 5368709120     # always keep ≥ 5 GiB free on the data FS
min_free_pct    = 5              # always keep ≥ 5% free on the data FS
node_max_bytes  = 0              # cap total bytes under data_directory (0 = unlimited)
```

`0` disables that check. You can mix all three — a PUT is refused if **any** of them would be violated by accepting it.

## What's checked

Before the chunker accepts the body:

- **`min_free_bytes`** — `statfs(data_directory).free_bytes` after the projected write must stay ≥ this.
- **`min_free_pct`** — `(free / total) * 100` after the write must stay ≥ this.
- **`node_max_bytes`** — recursive size of `data_directory` after the write must stay ≤ this. The walked size is cached and refreshed on a background sampler when metrics are enabled.

If any check fails, the server replies with the S3 error `InsufficientCapacity` (or the equivalent native API error) and the chunks already written for that request are unmarked from the pending tracker so GC will reclaim them on the next sweep.

## Why all three

- `min_free_bytes` handles small disks where 5% means a few hundred MB and Pebble's compaction needs more.
- `min_free_pct` handles big disks where 5% is meaningful and an absolute floor would be silly.
- `node_max_bytes` is the one to use when `data_directory` shares a filesystem with other tenants — Hangar will refuse to grow past its own quota rather than starving the neighbors.

## Visibility

When [metrics](/observability/metrics/) are enabled, a background sampler ticks the disk gauges:

- `hangar_disk_free_bytes`
- `hangar_disk_total_bytes`
- `hangar_disk_node_used_bytes`
- `hangar_disk_node_max_bytes`

Wire them into alerts so you know **before** the safeguard kicks in.
