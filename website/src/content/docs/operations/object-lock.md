---
title: Object Lock
description: WORM-style object retention and legal hold on the S3 API.
---

Hangar supports per-object retention (`GOVERNANCE` / `COMPLIANCE`) and legal hold via the standard S3 Object Lock API. Once enabled on a bucket, locked objects cannot be deleted or overwritten until retention expires (or legal hold is removed).

## Enable on a bucket

Object lock requires versioning. Enable versioning first (via the native admin API), then `PUT /:bucket?object-lock`:

```xml
<ObjectLockConfiguration>
  <ObjectLockEnabled>Enabled</ObjectLockEnabled>
  <Rule>
    <DefaultRetention>
      <Mode>GOVERNANCE</Mode>
      <Days>30</Days>
    </DefaultRetention>
  </Rule>
</ObjectLockConfiguration>
```

`<Rule>` is optional. If present, every `PutObject` without explicit lock headers inherits the default mode + computed `RetainUntilDate`.

Once `ObjectLockEnabled` is set on a bucket it cannot be turned off — same as S3.

Errors:

- `409 InvalidBucketState` — versioning is not enabled.
- `400 InvalidArgument` — bad `Mode` or missing/zero `Days`/`Years`.

`GET /:bucket?object-lock` returns the current config.

## Set lock on PUT

Three request headers, parsed on `PutObject` and (echoed on) `GetObject` / `HeadObject`:

| Header                                     | Value                             |
|--------------------------------------------|-----------------------------------|
| `x-amz-object-lock-mode`                   | `GOVERNANCE` or `COMPLIANCE`      |
| `x-amz-object-lock-retain-until-date`      | RFC3339 timestamp in the future   |
| `x-amz-object-lock-legal-hold`             | `ON` or `OFF`                     |

`mode` and `retain-until-date` must be set together. Legal hold is independent of retention.

If the bucket has a default retention rule, requests with no `-mode` / `-retain-until-date` headers receive the default automatically (legal hold is never defaulted).

## Modify lock on existing object

Per-object endpoints:

- `PUT /:bucket/:key?retention` — body `<Retention><Mode>…</Mode><RetainUntilDate>…</RetainUntilDate></Retention>`.
- `GET /:bucket/:key?retention` — returns the current retention or `404 NoSuchObjectLockConfiguration`.
- `PUT /:bucket/:key?legal-hold` — body `<LegalHold><Status>ON|OFF</Status></LegalHold>`.
- `GET /:bucket/:key?legal-hold` — returns `<Status>ON|OFF</Status>`.

All four accept `?versionId=` to target a specific version. Without it, the current version is updated (and the current pointer is kept in sync).

**Compliance is one-way**: once set, retention cannot be shortened and the mode cannot be downgraded to `GOVERNANCE`. Attempts return `403 AccessDenied`.

## Enforcement

| Action                                                  | Behavior                                       |
|---------------------------------------------------------|------------------------------------------------|
| `DeleteObject` without `versionId` (versioned bucket)   | Always succeeds — writes a delete marker; locked version is untouched |
| `DeleteObject` with `versionId` targeting a locked version | `403 AccessDenied`                          |
| `DeleteObjects` (batch) with a locked version           | Per-object `AccessDenied` in the result       |
| `PutObject` overwriting a locked object (versioning off) | `403 AccessDenied`                            |
| `PutObject` on a locked key (versioning on)             | Always succeeds — new version, old stays locked |

### Bypassing GOVERNANCE

`GOVERNANCE` is meant to be overridable by privileged operators. Hangar honors `x-amz-bypass-governance-retention: true` only when the request is signed by an S3 key with the `admin` permission. Any other key sees the header ignored.

`COMPLIANCE` mode is never bypassable, even by admin keys.

Legal hold blocks deletion regardless of mode and is independent of the bypass header. Remove it with `PUT … ?legal-hold` body `<LegalHold><Status>OFF</Status></LegalHold>`.

## Storage

- `BucketInfo.ObjectLockEnabled` is a flag persisted with the bucket record.
- Default retention lives at Pebble key `objectlock:<bucket>` as JSON.
- Per-object/version retention + legal hold are extra fields on `Metadatas` (`ObjectLockMode`, `ObjectLockRetainUntilMilli`, `ObjectLockLegalHold`).

All three are cleaned on `DeleteBucket`.

## Caveats

- Object lock cannot be enabled at `CreateBucket` time via the `x-amz-bucket-object-lock-enabled` header — use `PUT … ?object-lock` after enabling versioning.
- Lifecycle expiration on a versioned bucket creates a delete marker for the current version (S3-spec); the locked version itself is never physically removed by lifecycle.
- There is no separate compliance retention governance audit log — operations route through the regular S3 path.
