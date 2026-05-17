---
title: Host a single-page app
description: Step-by-step recipe to serve a SPA from Hangar with public bucket, website config, virtual-host, and CORS.
---

This guide walks through hosting a static SPA on Hangar with clean URLs, anonymous serve, and CORS-enabled API access from the same site.

## Goal

- `https://myapp.example.com/` → serves `index.html` (SPA shell)
- `https://myapp.example.com/assets/app.js` → serves the JS bundle
- Any 404 (e.g. `/not-a-route`) → falls back to `index.html` so the SPA router handles it
- A separate `api.example.com` calls signed S3 ops on the same Hangar

## 1. Enable virtual-host addressing

In `config.toml`:

```toml
[s3]
enabled           = true
bind_addr         = ":9000"
region            = "us-east-1"
virtual_host_base = "example.com"
```

DNS:

```
*.example.com    CNAME hangar.internal.
example.com      A     <hangar-ip>
```

## 2. Create the public bucket

```bash
hangar bucket create myapp --public
```

Bucket name must be a valid DNS label (lowercase, alphanumeric, hyphens). It will be reachable at `myapp.example.com`.

## 3. Wire static website hosting

```bash
hangar bucket website set myapp \
  --index index.html \
  --error index.html   # SPA fallback: 404 → SPA shell
```

The trick for SPA routing: set `--error` to the same `index.html`. Any GET on an unknown key returns the SPA shell with a 404 status — your client router takes over.

## 4. Allow your API origin (CORS)

If your SPA calls back to Hangar (e.g. signed PUT/GET to other buckets), add CORS:

```bash
cat > cors.json <<EOF
{
  "rules": [{
    "allowed_origins": ["https://myapp.example.com"],
    "allowed_methods": ["GET", "PUT", "POST", "DELETE", "HEAD"],
    "allowed_headers": ["*"],
    "expose_headers": ["ETag", "x-amz-version-id"],
    "max_age_seconds": 3600
  }]
}
EOF
hangar bucket cors set myapp --file cors.json
```

## 5. Upload your build

Use any S3 SDK / CLI. With `mc`:

```bash
mc alias set hangar https://myapp.example.com s3-key-id s3-secret --path-style=off
mc cp --recursive ./dist/ hangar/myapp/
```

Or with `aws`:

```bash
aws --endpoint-url https://example.com s3 sync ./dist/ s3://myapp/
```

## 6. Optional: enable access logs

```bash
hangar bucket create myapp-logs
hangar bucket logging set myapp \
  --target-bucket myapp-logs \
  --target-prefix access/
```

Each batch of requests is written as a plain-text object every 5s.

## 7. Optional: lifecycle on logs

```bash
cat > expire-logs.json <<EOF
{ "rules": [{ "enabled": true, "prefix": "access/", "expiration_days": 30 }] }
EOF
hangar bucket lifecycle set myapp-logs --file expire-logs.json
```

## Verify

```bash
curl https://myapp.example.com/             # → index.html
curl https://myapp.example.com/some/route   # → index.html (404 with SPA shell body)
curl https://myapp.example.com/assets/app.js  # → JS bundle
curl -I -H 'Origin: https://myapp.example.com' \
     -H 'Access-Control-Request-Method: PUT' \
     -X OPTIONS https://myapp.example.com    # → CORS preflight 204 with allow headers
```

## Related

- [Static website hosting](/operations/website/)
- [Virtual-host addressing](/operations/virtual-host/)
- [CORS](/operations/cors/)
- [Bucket access logging](/operations/bucket-logging/)
- [Lifecycle](/operations/lifecycle/)
