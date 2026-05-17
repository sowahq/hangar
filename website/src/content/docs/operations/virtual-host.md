---
title: Virtual-host addressing
description: Route S3 requests by subdomain (bucket.host) instead of path prefix (host/bucket).
---

By default, Hangar uses path-style addressing: `http://host:9000/bucket/key`. Enabling virtual-host addressing lets clients use `http://bucket.host:9000/key` instead.

This is required by some older SDKs (and the default for AWS SDK v1) and useful when fronting Hangar with a wildcard DNS + TLS certificate.

## Enable

In `config.toml`:

```toml
[s3]
enabled = true
bind_addr = ":9000"
region = "us-east-1"
virtual_host_base = "s3.example.com"
```

When `virtual_host_base` is empty (default), only path-style is accepted.

When set, requests with `Host: <bucket>.s3.example.com` (optionally with port) are routed as if the path were `/<bucket><origpath>`. Path-style still works in parallel — Hangar only rewrites when the Host has the configured suffix.

## DNS setup

For `virtual_host_base = "s3.example.com"`, point a **wildcard** A/AAAA/CNAME record to your Hangar instance:

```
*.s3.example.com  CNAME  hangar.internal.
s3.example.com    A      <hangar-ip>
```

If using TLS, you need a wildcard certificate for `*.s3.example.com`.

## SigV4 interaction

The client signs the request with the original path (`/key`) and Host (`bucket.s3.example.com`). Hangar rewrites the path internally for routing but preserves the original for SigV4 canonicalization — signatures verify against what the client actually signed.

## Client examples

AWS SDK Go v2:

```go
cli := s3.NewFromConfig(cfg, func(o *s3.Options) {
    o.BaseEndpoint = aws.String("http://s3.example.com:9000")
    o.UsePathStyle = false   // virtual-host
})
```

`mc`:

```bash
mc alias set hangar http://s3.example.com:9000 AK SK
mc cp file.txt hangar/mybucket/
```

`mc` defaults to virtual-host; pass `--path-style` to force path-style.

## Caveats

- Bucket names that don't form a valid DNS label (`UPPERCASE`, underscores, dots) cannot be virtual-host-addressed. Use path-style for those.
- A subdomain with a dot (`a.b.host`) is NOT recognized as a bucket. Bucket name must be a single DNS label.
