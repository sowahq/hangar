---
hide:
  - navigation
  - toc
---

# Hangar

<p style="font-size: 1.25rem; margin-top: -0.5rem;">
Self-hosted object storage in Go. Content-addressed, S3-compatible, simple to run.
</p>

[Get Started :material-rocket-launch:](getting-started.md){ .md-button .md-button--primary }
[GitHub :fontawesome-brands-github:](https://github.com/sowahq/hangar){ .md-button }

---

## Why Hangar

<div class="grid cards" markdown>

- :material-content-save-cog: __Content-addressed chunks__

    Blake3 hashes, zstd compression on Pebble. Dedup across uploads, immutable storage layout.

- :material-aws: __S3-compatible__

    Path-style routing, SigV4 (header + presigned + aws-chunked), multipart, CopyObject, DeleteObjects. Drop-in for most SDKs.

- :material-lock: __Server-side encryption__

    SSE-S3 (AES-256-GCM, server master key) and SSE-C (customer-provided key). AEAD + per-chunk nonces.

- :material-account-key: __Per-bucket auth__

    Argon2id tokens with `read` / `write` / `delete` / `admin` scopes. Bucket-restricted S3 keys.

- :material-tune-vertical: __Quotas, versioning, public buckets__

    Hard quotas on bytes/objects, opt-in object versioning, public-read mode for static assets.

- :material-recycle: __Background GC, healthcheck__

    Scheduled reclaim of unreferenced chunks. Deep `/status` probe (DB + disk + GC liveness).

</div>

---

## In 30 seconds

```sh
# build + run
make build
./bin/hangar server -c config.toml

# create a bucket
curl -X PUT http://localhost:8080/admin/buckets/photos

# issue a token, then upload
curl -X POST http://localhost:8080/admin/buckets/photos/tokens \
  -H 'Content-Type: application/json' -d '{"permissions":["write","read"]}'

curl -X PUT http://localhost:8080/photos/img.jpg \
  -H "Authorization: Bearer <token>" \
  --data-binary @img.jpg
```

See [Getting Started](getting-started.md) for the full walkthrough.

---

## Stack

Go 1.25 · [Fiber](https://gofiber.io) · [Pebble](https://github.com/cockroachdb/pebble) · [blake3](https://github.com/zeebo/blake3) · [zstd](https://github.com/klauspost/compress)
