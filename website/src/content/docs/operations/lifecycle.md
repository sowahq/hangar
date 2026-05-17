---
title: Lifecycle
description: Per-bucket expiration rules and abort-stale-multipart, S3 XML compatible.
---

Lifecycle in Hangar expires objects and aborts stale multipart uploads on a schedule. Rules are configured via the standard S3 XML on the `?lifecycle` subresource.

## Configure

Use any S3 SDK / CLI:

```sh
cat > lifecycle.json <<'EOF'
{
  "Rules": [
    {
      "ID": "expire-tmp",
      "Status": "Enabled",
      "Filter": { "Prefix": "tmp/" },
      "Expiration": { "Days": 7 },
      "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 1 }
    }
  ]
}
EOF

aws --endpoint-url http://localhost:9000 \
    s3api put-bucket-lifecycle-configuration \
    --bucket photos --lifecycle-configuration file://lifecycle.json
```

Or raw S3 XML on `PUT /:bucket?lifecycle`:

```xml
<LifecycleConfiguration>
  <Rule>
    <ID>expire-tmp</ID>
    <Status>Enabled</Status>
    <Filter><Prefix>tmp/</Prefix></Filter>
    <Expiration><Days>7</Days></Expiration>
    <AbortIncompleteMultipartUpload>
      <DaysAfterInitiation>1</DaysAfterInitiation>
    </AbortIncompleteMultipartUpload>
  </Rule>
</LifecycleConfiguration>
```

`GET /:bucket?lifecycle` returns the same document. `DELETE /:bucket?lifecycle` drops it.

Or via CLI (JSON file `{"rules":[{"enabled":true,"prefix":"logs/","expiration_days":30}]}`):

```bash
hangar bucket lifecycle set mybucket --file lifecycle.json
hangar bucket lifecycle get mybucket
hangar bucket lifecycle delete mybucket
```

## Supported fields

| Element                                                | Status |
|--------------------------------------------------------|--------|
| `Rule.ID`                                              | ✅     |
| `Rule.Status` (`Enabled` / `Disabled`)                 | ✅     |
| `Rule.Prefix` (top-level)                              | ✅     |
| `Rule.Filter.Prefix`                                   | ✅     |
| `Rule.Expiration.Days`                                 | ✅     |
| `Rule.AbortIncompleteMultipartUpload.DaysAfterInitiation` | ✅  |
| `Filter.Tag` / `Filter.And` / `Filter.ObjectSize…`     | ❌     |
| `Expiration.Date` / `Expiration.ExpiredObjectDeleteMarker` | ❌ |
| `NoncurrentVersionExpiration` / `Transition` / `NoncurrentVersionTransition` | ❌ |

Rule matching is **longest-prefix-wins**: an object key is checked against every enabled rule; the rule whose `Prefix` is the longest match applies.

## Scheduler

Lifecycle runs in two ways:

1. **Scheduled** — when `[lifecycle] enabled = true`, every `interval_hours` (default 24). One scan iterates every bucket that has a config, expires matching objects past their age, and aborts stale multiparts.
2. **On demand** — `POST /admin/lifecycle/run` triggers an immediate scan and returns the stats:

```json
{
  "BucketsScanned":    3,
  "ObjectsExpired":    142,
  "MultipartsAborted": 2
}
```

## Notes

- Expirations call the same delete path as `DELETE /:bucket/<key>`, which is versioning-aware: enabled buckets get a delete marker rather than a physical purge.
- Aborted multiparts are removed atomically: header + every part record, and the chunks they held reference become eligible for GC.
- Lifecycle does not free disk space directly — chunks come back into the GC sweep once their refcount reaches 0.
