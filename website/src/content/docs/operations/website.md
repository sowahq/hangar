---
title: Static website hosting
description: Serve buckets as static websites with index and error documents.
---

Hangar supports S3-style static website hosting. Mark a bucket public, configure `IndexDocument` and `ErrorDocument`, and anonymous GET requests are served without SigV4 authentication.

## Requirements

1. **Bucket must be public.** Set `public: true` at create time via the admin API:
   ```bash
   curl -X PUT "http://localhost:8080/admin/buckets/mysite" \
     -H "Content-Type: application/json" \
     -d '{"public": true}'
   ```
2. **Website configuration must be set.**

## Configure

Standard S3 XML on `PUT /:bucket?website`:

```xml
<WebsiteConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <IndexDocument><Suffix>index.html</Suffix></IndexDocument>
  <ErrorDocument><Key>error.html</Key></ErrorDocument>
</WebsiteConfiguration>
```

`IndexDocument.Suffix` is required. `ErrorDocument.Key` is optional but recommended.

- `GET /:bucket?website` returns the current configuration.
- `DELETE /:bucket?website` disables static hosting.

Or via CLI:

```bash
hangar bucket website set mysite --index index.html --error error.html
hangar bucket website get mysite
hangar bucket website delete mysite
```

## Serving behavior

When the bucket is public AND a website configuration exists, anonymous GET requests bypass SigV4 and are served as follows:

| Request                          | Response                                |
|----------------------------------|-----------------------------------------|
| `GET /:bucket`                   | `IndexDocument` object content          |
| `GET /:bucket/path/`             | `path/<IndexDocument>` object content   |
| `GET /:bucket/path/file.html`    | `file.html` object content              |
| Missing object + `ErrorDocument` | `ErrorDocument` content with `404`      |
| Missing object + no error doc    | Standard `NoSuchKey` 404                |

Authenticated SigV4 requests are routed normally — the website behavior only activates for anonymous traffic on public buckets.

## Tips

- Set explicit `Content-Type` headers when uploading HTML/CSS/JS via `PutObject ContentType` so browsers render correctly.
- Combine with [Virtual-host addressing](/operations/virtual-host/) for clean URLs like `https://mysite.example.com/`.
- Combine with [CORS](/operations/cors/) if your site fetches cross-origin resources.

## Caveats

- No redirect rules (S3's `RoutingRules` not supported).
- No host-name redirect (S3's `RedirectAllRequestsTo` not supported).
- Range requests, conditional headers, and other GetObject features are bypassed in website mode — anonymous serves a plain stream.
