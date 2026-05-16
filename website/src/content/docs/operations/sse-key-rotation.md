---
title: SSE key rotation
description: Rotate the SSE-S3 master key without breaking old objects.
---

Hangar keeps SSE-S3 master keys in a small keyring in Pebble. Objects record the id of the key they were sealed under, so rotating only affects **new** writes — old objects keep decrypting with their original key.

## Keyring

- `ssekr:keys:<id>` — raw 32-byte key + creation time.
- `ssekr:active` — id of the key used for new writes.

On first start, if `[security] master_key_b64` (or `HANGAR_MASTER_KEY`) is set and the ring is empty, Hangar seeds id `default` with that value and marks it active.

## List

```sh
curl http://localhost:8080/admin/sse/keys
```

```json
{
  "keys": [
    { "id": "default",                    "created_at": 1715000000000, "active": false },
    { "id": "k-3f2a8c1d9e07-1715817600",  "created_at": 1715817600000, "active": true  }
  ]
}
```

## Rotate

```sh
curl -X POST http://localhost:8080/admin/sse/keys/rotate
```

```json
{ "active_key_id": "k-3f2a8c1d9e07-1715817600" }
```

This:

1. Generates a fresh 32-byte random key.
2. Stores it as a new id (`k-<6 hex bytes>-<unix>`).
3. Sets `ssekr:active` to the new id.

From that point on, every new SSE-S3 PUT derives from the new master. Objects already on disk continue to work because their `Metadatas.SSEKeyID` still points at the old id, which is still in the ring.

## Activate a specific key

```sh
curl -X PUT http://localhost:8080/admin/sse/keys/default/activate
```

Useful if you rotated by mistake or want to pin writes to a specific id.

## What rotation does *not* do

- It does **not** re-key existing objects. To physically re-encrypt, copy them in place with `CopyObject` (which re-encrypts under the active key). This rebuilds chunks under the new key — expensive on large objects, and intra-object dedup will still work but cross-object dedup with the old-key version is lost.
- It does **not** revoke the old key from the ring. If you delete an old key, every object that recorded it as `SSEKeyID` becomes unreadable. Hangar does not expose a delete endpoint for this reason — manage the ring deliberately.
- It does **not** rotate SSE-C objects. SSE-C keys are not held by the server; only the client can rotate them, by reading + re-writing with a new key.

## Audit

Every rotate / activate is logged under `sse.rotate` / `sse.activate` if the [audit log](/operations/audit/) is enabled.
