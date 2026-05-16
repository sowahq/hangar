---
title: Bucket default encryption
description: Per-bucket default server-side encryption applied to uploads with no explicit SSE header.
---

Hangar supports per-bucket default server-side encryption on the S3 API. When configured, every `PutObject` / `CreateMultipartUpload` without an explicit `x-amz-server-side-encryption` header inherits the bucket default.

Only `AES256` (SSE-S3) is supported. `aws:kms` is rejected — Hangar has no KMS integration.

## Configure

Standard S3 XML on `PUT /:bucket?encryption`:

```xml
<ServerSideEncryptionConfiguration>
  <Rule>
    <ApplyServerSideEncryptionByDefault>
      <SSEAlgorithm>AES256</SSEAlgorithm>
    </ApplyServerSideEncryptionByDefault>
  </Rule>
</ServerSideEncryptionConfiguration>
```

Via SDK:

```sh
aws --endpoint-url http://localhost:9000 \
    s3api put-bucket-encryption --bucket photos \
    --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
```

`GET /:bucket?encryption` returns the config; `DELETE /:bucket?encryption` drops it.

## Behavior

- `PutObject` / `CreateMultipartUpload` **with** an `x-amz-server-side-encryption` header — request honored as-is.
- Request **with** SSE-C headers (`x-amz-server-side-encryption-customer-*`) — SSE-C honored; bucket default ignored. The two are mutually exclusive per S3 semantics.
- Request **without** any SSE header — bucket default applied (effectively the same as if the client had sent `x-amz-server-side-encryption: AES256`).

The SSE-S3 master key must be configured (`[security] master_key_b64` or `HANGAR_MASTER_KEY`); otherwise the inherited PUT fails with `503 ServerSideEncryptionConfigurationNotFoundError`, same as an explicit `AES256` would.

## Storage

The config is a small JSON blob under the Pebble key `encryption:<bucket>`. It is cleaned up on `DeleteBucket`.

## Caveats

- Only `AES256` is accepted. KMS keys (`<KMSMasterKeyID>`) are parsed and stored but never used — Hangar has no KMS path.
- `BucketKeyEnabled` is ignored.
- Pre-existing objects are not re-encrypted on configuration change.
