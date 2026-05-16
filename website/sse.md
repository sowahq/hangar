# Server-Side Encryption

Hangar supports two SSE modes on the S3 API:

- **SSE-S3** — server holds a master key, derives per-object keys via HKDF, transparent to clients.
- **SSE-C** — client provides a key on every request, server stores only its MD5.

Both modes use AES-256-GCM with a unique nonce per chunk.

## SSE-S3

### Setup

Generate and configure a 32-byte master key:

```sh
openssl rand -base64 32
```

```toml
[security]
master_key_b64 = "<your-32-byte-base64-key>"
```

Or via env: `HANGAR_MASTER_KEY=...`.

If unconfigured, requests using SSE-S3 return `503 ServerSideEncryptionConfigurationNotFoundError`.

### Usage

```sh
aws --endpoint-url http://localhost:9000 \
    s3 cp ./file.bin s3://photos/file.bin \
    --sse AES256
```

Or raw header: `x-amz-server-side-encryption: AES256`. The server echoes the header on `PUT`, `GET`, `HEAD`, `CopyObject`, and multipart `CompleteMultipartUpload` responses.

## SSE-C

### Usage

Three headers on every `PUT`, `GET`, `HEAD`, `UploadPart`:

| Header                                              | Value                              |
|-----------------------------------------------------|------------------------------------|
| `x-amz-server-side-encryption-customer-algorithm`   | `AES256`                           |
| `x-amz-server-side-encryption-customer-key`         | base64 of a 32-byte key            |
| `x-amz-server-side-encryption-customer-key-md5`     | base64 of MD5 of the raw key bytes |

```python
s3.put_object(
    Bucket="photos",
    Key="file.bin",
    Body=data,
    SSECustomerAlgorithm="AES256",
    SSECustomerKey=customer_key,            # boto3 computes the MD5
)

s3.get_object(
    Bucket="photos",
    Key="file.bin",
    SSECustomerAlgorithm="AES256",
    SSECustomerKey=customer_key,
)
```

The server stores **only** the MD5 of the key. You are responsible for keeping the key safe — lose it and the object is unrecoverable.

### Copy with SSE-C

To copy a SSE-C object, supply both the source key (via `x-amz-copy-source-server-side-encryption-customer-*` headers) and the destination key (standard SSE-C headers).

## Error matrix

| Situation                                                        | Status | Code                                              |
|------------------------------------------------------------------|--------|---------------------------------------------------|
| `PUT` with SSE-S3 but master key not configured                  | 503    | `ServerSideEncryptionConfigurationNotFoundError`  |
| `PUT` / `GET` with unsupported algorithm                         | 400    | `InvalidArgument`                                 |
| `GET` / `HEAD` of SSE-C object without customer headers          | 400    | `InvalidRequest`                                  |
| `GET` of SSE-C object with wrong key (MD5 mismatch)              | 400    | `InvalidArgument`                                 |
| SSE-C headers sent against an unencrypted object                 | 400    | `InvalidRequest`                                  |
| SSE-C headers sent against an SSE-S3 object                      | 400    | `InvalidRequest`                                  |

## Design notes

- Chunks are compressed → encrypted on write, decrypted → decompressed on read. Chunk hash is computed over the **sealed ciphertext**, so two identical plaintext chunks under different keys produce different chunk files. **Cross-object dedup is broken for encrypted objects.**
- Nonces are deterministic per chunk position: `prefix(4B) || u64be(partNumber<<40 | localChunkIndex)`. A 4-byte random prefix is generated per object (single PUT) or per multipart upload. Part number is 16 bits → no nonce reuse across parts even within the same upload.
- HKDF-SHA256 derives a per-object key from `(master_key, random_salt, "hangar-sse-s3")`. The salt is stored in object metadata.
- AEAD authentication failures surface as `InvalidArgument` on the GET path.

## Out of scope (v1)

- SSE-KMS
- Master key rotation / multi-key per server
- Bucket default encryption (`PUT /:bucket?encryption`)
- Cross-bucket dedup of encrypted chunks
