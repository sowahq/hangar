---
title: Bucket access logging
description: Async server access logs delivered to a target bucket as text objects.
---

Hangar supports S3-style bucket logging: each HTTP request to a source bucket is recorded and asynchronously batched into a target bucket as a plain-text object.

## Configure

Standard S3 XML on `PUT /:bucket?logging`:

```xml
<BucketLoggingStatus xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <LoggingEnabled>
    <TargetBucket>my-logs</TargetBucket>
    <TargetPrefix>access/source-bucket/</TargetPrefix>
  </LoggingEnabled>
</BucketLoggingStatus>
```

Disable by sending an empty `<BucketLoggingStatus/>` body.

Read with `GET /:bucket?logging`.

## Log delivery

- Records are buffered in memory and flushed every 5 seconds (or sooner if pressure builds).
- Each flush produces one object in the target bucket at key `<TargetPrefix><YYYY-MM-DD-HH-MM-SS>-<random>`.
- Recursive logging is prevented: if `TargetBucket == SourceBucket`, records are dropped.
- Writes to the target bucket itself are NOT re-logged (writer uses the internal service layer, bypasses HTTP middleware).
- Logging configuration is cleared when the source bucket is deleted.

## Record format

Plain text, one record per line. Inspired by the AWS S3 Server Access Log Format but simplified:

```
- <bucket> [<time>] <remote-ip> <access-key> <req-id> <method> <key> "<method> <path> HTTP/1.1" <status> - <bytes-sent> <object-size> <total-ms> - "<user-agent>" "<referer>" -
```

Missing fields are emitted as `-`. Times are UTC in `[dd/Mon/yyyy:HH:MM:SS +0000]` format.

## Caveats

- Logs are written best-effort. If the target bucket disappears or PutObject fails, the batch is dropped (warning logged to server logs).
- No durable queue — records held in memory between flushes are lost on crash.
- The target bucket must exist before enabling logging.
