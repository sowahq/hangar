---
title: CORS
description: Per-bucket CORS config for browser clients on the S3 API.
---

Hangar honors per-bucket CORS on the S3 API. Useful when a browser SPA needs to PUT/GET objects directly against Hangar.

## Configure

Standard S3 XML on `PUT /:bucket?cors`:

```xml
<CORSConfiguration>
  <CORSRule>
    <ID>spa</ID>
    <AllowedOrigin>https://app.example.com</AllowedOrigin>
    <AllowedOrigin>https://*.preview.example.com</AllowedOrigin>
    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <ExposeHeader>ETag</ExposeHeader>
    <MaxAgeSeconds>3600</MaxAgeSeconds>
  </CORSRule>
</CORSConfiguration>
```

Via SDK:

```sh
aws --endpoint-url http://localhost:9000 \
    s3api put-bucket-cors --bucket photos --cors-configuration file://cors.json
```

`GET /:bucket?cors` returns the config; `DELETE /:bucket?cors` drops it.

Or via CLI (with a JSON file like `{"rules":[{"allowed_origins":["*"],"allowed_methods":["GET"]}]}`):

```bash
hangar bucket cors set photos --file cors.json
hangar bucket cors get photos
hangar bucket cors delete photos
```

## Preflight & response

The S3 router has two pieces:

- **`OPTIONS /:bucket[/*]`** — preflight handler. Reads `Origin` and `Access-Control-Request-{Method,Headers}`, picks the first matching rule, and replies with the appropriate `Access-Control-*` headers + `200`. Returns `403` if no rule matches.
- **Response middleware** — on every cross-origin (non-`OPTIONS`) S3 request, if the bucket has a CORS config and the request matches a rule, the response gets `Access-Control-Allow-Origin`, `Vary: Origin`, `Access-Control-Allow-Methods`, and (if applicable) `Access-Control-Expose-Headers` / `Access-Control-Max-Age`.

## Origin matching

- `*` matches anything and the response uses `Access-Control-Allow-Origin: *`.
- `*.example.com` matches a single subdomain label segment (glob `*` is `[^.]+`).
- An exact match returns the exact origin (so credentials work).

## Notes

- CORS lives only on the S3 port. The native HTTP API is meant for backends / scripts, not browsers, so it does not run a CORS layer.
- Preflight and response middleware run **outside** SigV4 — `OPTIONS` is exempt from signing. The actual request that follows must be SigV4-signed by the browser (e.g. via presigned URL or a signed XHR).
- Deleting the bucket also deletes its CORS config.
