---
title: Bucket and object tagging
description: Attach key-value tags to buckets and objects via the S3 tagging API or the x-amz-tagging header at upload time.
---

Hangar implements bucket-level and object-level tagging compatible with the S3 tagging surface (`?tagging` subresource + `x-amz-tagging` PUT header).

## Limits

- Max **10 tags** per resource (bucket or object).
- Tag key: 1–128 characters.
- Tag value: 0–256 characters.
- Duplicate keys rejected (`InvalidTag` 400).

## Bucket tagging

```
PUT    /:bucket?tagging   (Tagging XML body)
GET    /:bucket?tagging   → Tagging XML
DELETE /:bucket?tagging
```

Example PUT body:

```xml
<Tagging xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <TagSet>
    <Tag><Key>env</Key><Value>prod</Value></Tag>
    <Tag><Key>team</Key><Value>platform</Value></Tag>
  </TagSet>
</Tagging>
```

Tags are removed when the bucket is deleted.

## Object tagging

```
PUT    /:bucket/:key?tagging[&versionId=<id>]
GET    /:bucket/:key?tagging[&versionId=<id>]
DELETE /:bucket/:key?tagging[&versionId=<id>]
```

Same XML format. When `versionId` is supplied on a versioned bucket, the tag mutation targets that specific version. Without `versionId`, the latest version is updated.

## `x-amz-tagging` header

`PutObject` accepts `x-amz-tagging` as a URL-encoded query string and persists the tags alongside the new object:

```
PUT /:bucket/:key
x-amz-tagging: env=prod&owner=mathis
```

Equivalent to a `PutObject` followed by a `PutObjectTagging`, but in a single request.

## `x-amz-tagging-count` echo

`GetObject` and `HeadObject` set `x-amz-tagging-count: <N>` on the response when the object has tags. Useful for clients that want to know without an extra round-trip.

## Note

Tagging is metadata only — Hangar does not yet evaluate tag-based policies (lifecycle rules, IAM-style conditions). Tags persist and round-trip via the API.
