---
title: Browser direct uploads (POST policy)
description: Upload objects directly from a browser using a SigV4-signed POST policy form.
---

POST policy lets a browser upload directly to Hangar without proxying bytes through your application backend. Your backend signs a short-lived policy; the browser submits a `multipart/form-data` POST to the bucket URL.

## Flow

1. Backend builds a JSON policy with `expiration` + `conditions`.
2. Backend base64-encodes the policy and signs it using SigV4 with the S3 access key's secret.
3. Backend returns the form fields (policy, signature, credential, date, key, etc.) to the browser.
4. Browser builds a `multipart/form-data` form with those fields plus the file and POSTs to `http://endpoint/:bucket`.

## Endpoint

```
POST /:bucket
Content-Type: multipart/form-data; boundary=...
```

## Required form fields

| Field             | Notes                                                |
|-------------------|------------------------------------------------------|
| `key`             | Object key. May contain `${filename}` placeholder    |
| `policy`          | Base64-encoded JSON policy document                  |
| `x-amz-algorithm` | Must be `AWS4-HMAC-SHA256`                           |
| `x-amz-credential`| `<access-key>/<date>/<region>/s3/aws4_request`       |
| `x-amz-date`      | Same as the date used in the credential scope        |
| `x-amz-signature` | hex(HMAC-SHA256(signingKey, policy_base64))          |
| `file`            | The file part (must be last for compatibility)       |

Optional: `Content-Type`, `success_action_status` (set to `201` for `201 Created`, default `204`).

`${filename}` in the `key` field is replaced by the uploaded file's original name. Policy conditions are evaluated against the **raw** key (template), not the substituted result.

## Policy document

```json
{
  "expiration": "2026-05-20T00:00:00Z",
  "conditions": [
    {"bucket": "mybucket"},
    ["starts-with", "$key", "uploads/"],
    ["content-length-range", 0, 1048576]
  ]
}
```

Supported conditions:

- `{"<field>": "<value>"}` — exact match on `<field>` form value
- `["eq", "$<field>", "<value>"]` — exact match
- `["starts-with", "$<field>", "<prefix>"]` — prefix match
- `["content-length-range", <min>, <max>]` — file size bounds

## SDK presigner

AWS SDK Go v2 builds the form for you:

```go
pc := s3.NewPresignClient(cli)
pp, _ := pc.PresignPostObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String("mybucket"),
    Key:    aws.String("uploads/${filename}"),
})
// pp.URL → POST endpoint
// pp.Values → map of form fields to put into multipart form alongside the file
```

## Responses

- `204 No Content` on success (default).
- `201 Created` if `success_action_status=201` is in the form.
- `400 MalformedPOSTRequest` for missing required fields.
- `403 SignatureDoesNotMatch` for bad signature.
- `403 AccessDenied` for expired policy or condition mismatch.
