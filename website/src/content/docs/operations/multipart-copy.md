---
title: Server-side multipart copy
description: Copy a source object (or byte range) into a multipart upload part without re-uploading the bytes from the client.
---

`UploadPartCopy` lets you assemble a destination object from byte ranges of existing source objects entirely server-side. The client never re-transfers the bytes.

## Endpoint

```
PUT /:dstBucket/:dstKey?partNumber=<N>&uploadId=<id>
x-amz-copy-source: /<srcBucket>/<srcKey>[?versionId=<v>]
x-amz-copy-source-range: bytes=<start>-<end>   (optional)
```

The body MUST be empty. Source bytes are streamed from chunk storage, re-chunked under the destination multipart upload's SSE configuration, and registered as part `<N>`.

## Response

```xml
<CopyPartResult>
  <ETag>"<part-etag>"</ETag>
  <LastModified>2026-05-17T02:00:00Z</LastModified>
</CopyPartResult>
```

If the source has a `VersionId`, it's echoed in the `x-amz-copy-source-version-id` response header.

## Range

`x-amz-copy-source-range: bytes=START-END` (inclusive, byte offsets into the source object). When omitted, the entire source is copied. Range must satisfy `0 ≤ start ≤ end < srcSize`.

## SSE

The destination upload's SSE configuration applies. If the destination upload is SSE-S3 or SSE-C, the copied bytes are re-encrypted with the destination's keys. SSE-C requires `x-amz-server-side-encryption-customer-*` headers (the same set provided at `CreateMultipartUpload`).

For the source, if it's encrypted with SSE-C, supply `x-amz-copy-source-server-side-encryption-customer-*` headers.

## Conditional copy

Same source preconditions as `CopyObject`:

| Header                                  | Behavior                              |
|-----------------------------------------|---------------------------------------|
| `x-amz-copy-source-if-match`            | Source ETag must match                |
| `x-amz-copy-source-if-none-match`       | Source ETag must NOT match            |
| `x-amz-copy-source-if-modified-since`   | Source modified since                 |
| `x-amz-copy-source-if-unmodified-since` | Source NOT modified since             |

Mismatch yields `412 PreconditionFailed`. See [Conditional requests](/operations/conditional-requests/).

## Use case

Building large objects from existing ranges without re-transferring data — e.g. log rollups, concatenating archive shards, or stitching uploads from multiple clients into a single object via a multipart workflow.
