---
title: S3 compatibility
description: Exhaustive status of S3 operations, subresources, headers, and known gaps.
---

This page is the canonical answer to "does Hangar support _X_?". For setup and SDK examples, see [S3 API](/api/s3/).

## Addressing

| Style                                               | Status |
|-----------------------------------------------------|--------|
| Path-style (`http://host:9000/bucket/key`)          | ✅     |
| Virtual-host (`http://bucket.host:9000/key`)        | ❌     |

Configure SDKs with `UsePathStyle: true` (Go SDK v2), `s3={"addressing_style":"path"}` (boto3), `--use-path-style` (`mc`), etc.

## Authentication

| Mechanism                              | Status |
|----------------------------------------|--------|
| SigV4 in `Authorization` header         | ✅     |
| SigV4 in presigned URL query            | ✅     |
| `aws-chunked` streaming payload (SigV4) | ✅     |
| SigV2 (legacy)                          | ❌     |
| Anonymous (public-read buckets)         | ⚠️ supported via native API only — S3 path always requires SigV4 |

## Bucket operations

| Operation                                    | Status | Notes                                                |
|----------------------------------------------|--------|------------------------------------------------------|
| `ListBuckets`                                | ✅     |                                                      |
| `CreateBucket` (`PUT /:bucket`)              | ✅     | No location constraint, no ACL                       |
| `DeleteBucket`                               | ✅     | Must be empty                                        |
| `HeadBucket`                                 | ✅     |                                                      |
| `GetBucketLocation`                          | ❌     | Returns the server's configured `region` implicitly through SigV4 |
| `GetBucketAcl` / `PutBucketAcl`              | ❌     |                                                      |
| `GetBucketPolicy` / `PutBucketPolicy`        | ❌     |                                                      |
| `GetBucketVersioning` / `PutBucketVersioning`| ❌     | Versioning is toggled via the native admin API       |
| `GetBucketTagging` / `PutBucketTagging`      | ❌     |                                                      |
| `GetBucketLogging` / `PutBucketLogging`      | ❌     |                                                      |
| `GetBucketCors` / `PutBucketCors` / `DeleteBucketCors` | ✅ | See [CORS](/operations/cors/)                  |
| `GetBucketLifecycleConfiguration` / `PutBucketLifecycleConfiguration` / `DeleteBucketLifecycle` | ✅ | Expiration + AbortIncompleteMultipartUpload. See [Lifecycle](/operations/lifecycle/) |
| `GetBucketEncryption` / `PutBucketEncryption` / `DeleteBucketEncryption` | ✅ | AES256 (SSE-S3) only. See [Bucket default encryption](/operations/bucket-encryption/) |
| `GetBucketNotificationConfiguration` / `Put…`| ❌     | No event hooks                                       |
| `PutBucketReplication` / `Get…` / `Delete…`  | ❌     | Planned with the upcoming distribution work          |
| `GetBucketWebsite` / `PutBucketWebsite`      | ❌     |                                                      |
| `GetBucketRequestPayment` / `PutBucketRequestPayment` | ❌ |                                              |
| `GetObjectLockConfiguration` / `PutObjectLockConfiguration` | ✅ | Requires versioning. GOVERNANCE / COMPLIANCE modes. See [Object Lock](/operations/object-lock/) |

## Object operations

| Operation                                | Status | Notes                                                       |
|------------------------------------------|--------|-------------------------------------------------------------|
| `PutObject`                              | ✅     | Including `aws-chunked` streaming. SSE-S3 + SSE-C honored. `If-Match` and `If-None-Match: *` preconditions |
| `GetObject`                              | ✅     | `Range`, `If-Match`, `If-None-Match`, `If-Modified-Since`, `If-Unmodified-Since` |
| `HeadObject`                             | ✅     | `If-Match`, `If-None-Match`, `If-Modified-Since`, `If-Unmodified-Since` |
| `DeleteObject`                           | ✅     | Versioning-aware                                            |
| `DeleteObjects` (batch)                  | ✅     | Up to S3 default batch size                                 |
| `CopyObject`                             | ✅     | Chunk-ref reuse when both sides unencrypted; full re-encrypt across SSE keys |
| `ListObjectsV2`                          | ✅     | `prefix`, `delimiter`, `start-after`, `continuation-token`, `max-keys` |
| `ListObjects` (v1)                       | ❌     | Use V2                                                      |
| `ListObjectVersions`                     | ✅     | `GET /:bucket?versions` with `prefix`, `delimiter`, `key-marker`, `version-id-marker`, `max-keys` |
| `GetObjectAcl` / `PutObjectAcl`          | ❌     |                                                             |
| `GetObjectTagging` / `PutObjectTagging`  | ❌     |                                                             |
| `GetObjectAttributes`                    | ❌     | Use `HeadObject`                                            |
| `RestoreObject`                          | ❌     | No tiers                                                    |
| `SelectObjectContent`                    | ❌     |                                                             |
| `GetObjectLegalHold` / `PutObjectLegalHold` | ✅  | `Status: ON/OFF`. See [Object Lock](/operations/object-lock/)  |
| `GetObjectRetention` / `PutObjectRetention` | ✅  | GOVERNANCE bypassable with admin key + `x-amz-bypass-governance-retention: true`. COMPLIANCE never bypassable |

## Multipart upload

| Operation                       | Status | Notes                                                         |
|---------------------------------|--------|---------------------------------------------------------------|
| `CreateMultipartUpload`         | ✅     | SSE config captured at init, inherited by every part          |
| `UploadPart`                    | ✅     | SSE-C requires the customer key headers on every part         |
| `UploadPartCopy`                | ✅     | `x-amz-copy-source` with optional `x-amz-copy-source-range`. Re-encrypts when destination upload has SSE configured |
| `CompleteMultipartUpload`       | ✅     | Writes a single `Metadatas` entry; for SSE objects, part boundaries are recorded so the reader can derive per-chunk nonces |
| `AbortMultipartUpload`          | ✅     | Lifecycle `AbortIncompleteMultipartUpload` triggers this automatically |
| `ListMultipartUploads`          | ✅     |                                                               |
| `ListParts`                     | ✅     |                                                               |

## Presigned URLs

| Operation | Status |
|-----------|--------|
| GET       | ✅     |
| PUT       | ✅     |
| POST policy | ❌    |
| Multipart presigned | ❌ |

## Headers honored

### Server-side encryption

| Header                                                           | On      |
|------------------------------------------------------------------|---------|
| `x-amz-server-side-encryption: AES256`                           | PUT / GET / HEAD / CopyObject (source + dest variants) / CreateMultipartUpload (echoed) |
| `x-amz-server-side-encryption-customer-algorithm`                | PUT / GET / HEAD / UploadPart / CopyObject                                              |
| `x-amz-server-side-encryption-customer-key`                      | …                                                                                       |
| `x-amz-server-side-encryption-customer-key-MD5`                  | …                                                                                       |
| `x-amz-copy-source-server-side-encryption-customer-algorithm`    | CopyObject source                                                                       |
| `x-amz-copy-source-server-side-encryption-customer-key`          | …                                                                                       |
| `x-amz-copy-source-server-side-encryption-customer-key-MD5`      | …                                                                                       |
| `x-amz-server-side-encryption-aws-kms-key-id`                    | ❌ (no SSE-KMS)                                                                         |

### Checksums

| Header                                | Status |
|---------------------------------------|--------|
| `x-amz-sdk-checksum-algorithm` (request hint) | ✅ parsed, persisted |
| `x-amz-checksum-crc32`                | ✅ persisted + echoed on GET/HEAD/PUT/UploadPart/Copy |
| `x-amz-checksum-crc32c`               | ✅                                                    |
| `x-amz-checksum-crc64nvme`            | ✅                                                    |
| `x-amz-checksum-sha1`                 | ✅                                                    |
| `x-amz-checksum-sha256`               | ✅                                                    |
| `x-amz-checksum-type: FULL_OBJECT`    | ✅ echoed                                             |

Hangar trusts the client-provided value and echoes it. It does not recompute, so SDKs that compute and send checksums for integrity will see them round-trip; SDKs that expect Hangar to compute on its own do not get one.

### CopyObject

| Header                            | Status |
|-----------------------------------|--------|
| `x-amz-copy-source`               | ✅     |
| `x-amz-metadata-directive`        | ⚠️ accepted, content-type/encoding semantics best-effort |
| `x-amz-copy-source-if-match` / `-if-none-match` / `-if-modified-since` / `-if-unmodified-since` | ❌ |

### Range / conditional GET

| Header              | Status |
|---------------------|--------|
| `Range: bytes=…`    | ✅ single range, returns `206 Partial Content`, multipart ranges not supported |
| `If-Modified-Since` | ✅     |
| `If-None-Match`     | ✅     |
| `If-Match`          | ❌     |
| `If-Unmodified-Since` | ❌   |

### Object lock headers

| Header                                       | On      |
|----------------------------------------------|---------|
| `x-amz-object-lock-mode`                     | PUT object / GET / HEAD (echoed) |
| `x-amz-object-lock-retain-until-date`        | PUT object (RFC3339) / GET / HEAD (echoed) |
| `x-amz-object-lock-legal-hold`               | PUT object (`ON`/`OFF`) / GET / HEAD (echoed when `ON`) |
| `x-amz-bypass-governance-retention`          | DELETE object / DeleteObjects — requires admin key; never bypasses COMPLIANCE |

### Bucket encryption XML

`PUT /:bucket?encryption` accepts:

```xml
<ServerSideEncryptionConfiguration>
  <Rule>
    <ApplyServerSideEncryptionByDefault>
      <SSEAlgorithm>AES256</SSEAlgorithm>
    </ApplyServerSideEncryptionByDefault>
  </Rule>
</ServerSideEncryptionConfiguration>
```

Only `AES256` is supported. `aws:kms` is rejected. When configured, `PutObject` and `CreateMultipartUpload` without an explicit `x-amz-server-side-encryption` header inherit the bucket default. Explicit SSE headers (including SSE-C) on the request are not overridden.

### Object lock XML

`PUT /:bucket?object-lock` accepts:

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

Versioning must be enabled on the bucket first (returns `409 InvalidBucketState` otherwise). Once enabled, object lock cannot be disabled. `PUT /:bucket/:key?retention` and `PUT /:bucket/:key?legal-hold` apply per-object retention.

### Lifecycle XML

`PUT /:bucket?lifecycle` accepts:

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

Supported: `Prefix` (top-level or under `Filter`), `Status`, `Expiration.Days`, `AbortIncompleteMultipartUpload.DaysAfterInitiation`.

Not supported: `Filter.Tag`, `Filter.And`, `Filter.ObjectSizeGreaterThan` / `-LessThan`, `Expiration.Date`, `Expiration.ExpiredObjectDeleteMarker`, `NoncurrentVersionExpiration`, `Transition` / `NoncurrentVersionTransition` (no storage tiers).

### CORS XML

`PUT /:bucket?cors` accepts the standard `CORSConfiguration` document. Wildcards in `AllowedOrigin` are matched as globs (`*.example.com` works). `MaxAgeSeconds`, `ExposeHeader` are honored. See [CORS](/operations/cors/).

## Error format

Errors are XML in the standard S3 envelope:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>bucket not found</Message>
  <Resource>/missing-bucket</Resource>
  <RequestId></RequestId>
</Error>
```

Codes commonly seen: `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `RequestTimeTooSkewed`, `NoSuchBucket`, `NoSuchKey`, `BucketAlreadyOwnedByYou`, `BucketNotEmpty`, `InvalidArgument`, `InvalidRequest`, `MalformedXML`, `NotImplemented`, `ServerSideEncryptionConfigurationNotFoundError`.

## Interop test harness

`tools/s3interop/` is a small Go program that drives `aws-sdk-go-v2` against a running Hangar (and optionally against real AWS S3) to exercise the supported surface. Run it after non-trivial changes to the S3 layer:

```sh
cd tools/s3interop
go run . --endpoint http://localhost:9000 --access-key AK --secret-key SK
```
