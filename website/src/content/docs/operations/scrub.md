---
title: Integrity scrub
description: Re-hash every chunk to detect bit rot, quarantine corrupt files, report dangling references.
---

Hangar stores chunks as `data/chunks/<aa>/<bb>/<blake3-hex>`. Bit rot, half-written files after a crash, or a wayward `rm` will silently break a future `GetObject`. The scrub re-hashes everything and reports the gap.

## Run once

```sh
./bin/hangar scrub run -c config.toml
./bin/hangar scrub run -c config.toml --dry-run
./bin/hangar scrub run -c config.toml --rate 52428800   # 50 MiB/s throttle
```

The server must be **stopped** (Pebble lock). Output:

```json
{
  "TotalChunks":    12483,
  "BytesScanned":   9842710334,
  "Corrupted":      0,
  "Quarantined":    0,
  "MissingFiles":   0,
  "DanglingRefs":   0,
  "StartedAt":      "2026-05-16T10:30:00Z",
  "Duration":       324012987654
}
```

What it does, per chunk file:

1. Re-hashes with blake3.
2. Compares to the filename.
3. If mismatched: moves to `data/chunks/.corrupted/` (unless `--dry-run`), increments `Corrupted` + `Quarantined`.
4. Cross-references the `chunkref:` index: counts `MissingFiles` (refcount > 0 but file gone) and `DanglingRefs` (refcount entries pointing nowhere).

Quarantined chunks are not deleted — recover what you can from backups, then `rm -rf data/chunks/.corrupted` once you have decided.

## Run on a schedule

```toml
[scrub]
interval_hours      = 168       # weekly
rate_bytes_per_sec  = 0          # 0 = unlimited
```

This runs inside the live server. The rate limiter keeps scrub from saturating disk I/O during operating hours.

Prometheus counters reflect both modes — see [Metrics](/observability/metrics/) for `hangar_scrub_*`.

## When to scrub

- After hardware events (controller swap, disk migration, ungraceful power loss).
- After a restore from backup, to confirm the chunk tree round-tripped intact.
- On a slow rolling cadence (weekly / monthly) against silent bit rot on consumer SSDs.

## When not to

- The scrub touches every byte of every chunk. On large stores, that is expensive. Throttle with `--rate` (or `[scrub] rate_bytes_per_sec`) on shared hardware.
- Do not run two scrubs in parallel — they share the quarantine directory and `chunkref:` view.
