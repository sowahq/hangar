---
title: Backup & restore
description: Consistent offline backup of Pebble + chunks, and how to restore.
---

Hangar ships a CLI for consistent offline backups. The server must be **stopped** during backup — Pebble holds an exclusive lock.

## Create

```sh
./bin/hangar backup create -c config.toml -o ./snapshots/2026-05-16
```

What it does:

1. Reads `data_directory` from the config.
2. Opens the local Pebble store and calls `Checkpoint(<out>/store)` — a Pebble-consistent snapshot using hard links where possible.
3. Hard-links (or copies, across filesystems) every file under `data/chunks/` into `<out>/chunks/`.
4. Writes `<out>/manifest.json`:

```json
{
  "version": 1,
  "created_at": "2026-05-16T10:23:00Z",
  "store_bytes": 12453421,
  "chunk_bytes": 9842710334,
  "chunk_files": 12483
}
```

The destination directory **must not exist** beforehand. Hard links make the operation essentially free in disk space on the same filesystem; cross-FS falls back to copy.

## Restore

```sh
./bin/hangar backup restore -c config.toml -i ./snapshots/2026-05-16
```

What it does:

1. Reads `data_directory` from the config.
2. Reads `<in>/manifest.json` (rejects unknown `version`).
3. Refuses to proceed if the target `data_directory` already contains `store/` or `chunks/`.
4. Clones `<in>/store` → `<data>/store` and `<in>/chunks` → `<data>/chunks`.

Start the server afterwards.

## Online backups

Not supported natively. If you need a hot snapshot:

- Run on ZFS / btrfs / LVM and snapshot the volume holding `data_directory`. Pebble's WAL + atomic rename make this safe enough in practice; you take responsibility for the snapshot's atomicity.
- Or schedule a stop / `backup create` / start window — chunks are content-addressed so concurrent uploads after restart never collide with restored bytes.

## Disaster recovery checklist

1. Stop the server (`SIGTERM`, wait for graceful shutdown).
2. `hangar backup create -o /backup/$(date +%F)`.
3. Restart.
4. Periodically run `hangar scrub run` against the restored copy to confirm chunk integrity (see [Scrub](/operations/scrub/)).
5. Test a restore into a scratch directory — backups you never restored are not backups.
