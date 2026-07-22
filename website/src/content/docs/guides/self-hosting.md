---
title: Self-hosting with Docker
description: Recommended production setup for a single-node self-hosted Hangar — Docker Compose, volume permissions, hardened config, buckets and keys.
---

This guide walks you from zero to a working self-hosted Hangar: a Docker Compose file, correct volume permissions, a hardened `config.toml`, your first bucket, and S3 access from any client.

## 1. Directory layout and permissions

The image runs as a non-root user (`uid 10001`). Your bind-mounted data directory must be readable **and writable** by that user, otherwise the server fails at startup with:

```
open /data/config.toml: permission denied
```

Create the directory and hand it to the container user once:

```sh
mkdir -p /home/docker/hangar/data
sudo chown -R 10001:10001 /home/docker/hangar/data
```

If you keep all your container binds under one root (e.g. `/home/docker`) and want new files to inherit the right permissions automatically, use a default ACL instead:

```sh
sudo setfacl -R  -m u:10001:rwX /home/docker/hangar
sudo setfacl -R -dm u:10001:rwX /home/docker/hangar
```

## 2. Docker Compose

```yaml
services:
  hangar:
    image: ghcr.io/sowahq/hangar:latest
    container_name: hangar
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"   # native HTTP API + admin — keep local
      - "9000:9000"             # S3 API — safe to expose (SigV4 auth)
    volumes:
      - /home/docker/hangar/data:/data
    environment:
      - HANGAR_MASTER_KEY=${HANGAR_MASTER_KEY}
      - HANGAR_ADMIN_TOKEN=${HANGAR_ADMIN_TOKEN}
```

Two ports, two rules:

- **`8080` (native API)** hosts the unauthenticated `/admin/*` endpoints. Bind it to `127.0.0.1` (as above) or a private network. If you need it remotely, put a reverse proxy with auth in front.
- **`9000` (S3 API)** requires a valid SigV4 signature on every request, so it can face the internet — ideally behind a TLS-terminating proxy (Caddy, Traefik, nginx).

Generate the master key (SSE-S3 encryption) and the admin token (protects `/admin/*` and is picked up automatically by the CLI), and put both in a `.env` file next to the compose file:

```sh
echo "HANGAR_MASTER_KEY=$(openssl rand -base64 32)" > .env
echo "HANGAR_ADMIN_TOKEN=$(openssl rand -hex 32)" >> .env
chmod 600 .env
```

The container image ships a built-in `HEALTHCHECK` against `GET /healthz`, so `docker ps` (and tools like Dokploy) report real container health out of the box.

## 3. Recommended config.toml

Hangar generates a minimal config on first start, but the defaults are tuned for a quick local try, not for a server. Write this to `/home/docker/hangar/data/config.toml` before the first start:

```toml
data_directory = "data"

[api]
bind_addr = ":8080"

[storage]
chunk_size = 4194304
enable_compression = true

# Refuse writes before the disk fills up — a full store can corrupt.
min_free_pct = 5

# Durable metadata writes. Keep true unless the host is UPS-backed.
sync_writes = true

[garbage_collection]
interval_hours = 24

[scrub]
# Weekly background integrity check, throttled to stay gentle on the disk.
interval_hours     = 168
rate_bytes_per_sec = 52428800   # 50 MiB/s

[s3]
enabled   = true
bind_addr = ":9000"
region    = "us-east-1"

[lifecycle]
# Expires objects and aborts stale multipart uploads per bucket rules.
enabled        = true
interval_hours = 24
```

Then start it:

```sh
docker compose up -d
docker compose logs -f hangar
```

See [Configuration](/configuration/) for every knob (rate limiting, audit log, Prometheus metrics, virtual-host addressing).

## 4. Create your first bucket

The `hangar` binary inside the container doubles as a CLI that talks to the local admin API:

```sh
docker exec hangar hangar bucket create photos
docker exec hangar hangar bucket list
```

Add `--public` to make a bucket readable without auth (e.g. for [static website hosting](/operations/website/)):

```sh
docker exec hangar hangar bucket create assets --public
```

Toggle visibility on an existing bucket anytime:

```sh
docker exec hangar hangar bucket public assets
docker exec hangar hangar bucket public assets --off
```

Cap a bucket's size or object count with a quota (`0` = unlimited):

```sh
docker exec hangar hangar bucket quota photos --max-bytes 10737418240
```

Writes are refused once the quota is reached. When running the CLI from outside the container, point it at the server once with `HANGAR_SERVER=http://your-server:8080` instead of repeating `--server`.

## 5. Create S3 credentials

Generate an access key / secret key pair for your S3 clients:

```sh
docker exec hangar hangar s3keys create -p read -p write -p delete
```

The `secret_key` is printed **once** — store it now. To scope a key to specific buckets, repeat `-b`:

```sh
docker exec hangar hangar s3keys create -p read -p write -b photos -b assets
```

Permissions are `read`, `write`, `delete`, and `admin` (bucket creation and deletion over S3).

## 6. Use it from any S3 client

Point any S3-compatible tool at port `9000` with the key pair from the previous step.

**AWS CLI:**

```sh
export AWS_ACCESS_KEY_ID="<access_key_id>"
export AWS_SECRET_ACCESS_KEY="<secret_key>"

aws s3 cp photo.jpg s3://photos/ --endpoint-url http://your-server:9000
aws s3 ls s3://photos/ --endpoint-url http://your-server:9000
```

**rclone** (`~/.config/rclone/rclone.conf`):

```ini
[hangar]
type = s3
provider = Other
access_key_id = <access_key_id>
secret_access_key = <secret_key>
endpoint = http://your-server:9000
```

```sh
rclone copy ./backups hangar:photos/backups
```

Any SDK works the same way — set the endpoint, force path-style addressing if the SDK asks, and use the key pair. See [S3 API](/api/s3/) for per-language snippets.

## 7. Day-2 checklist

- **Backups** — snapshot the data directory with the [backup & restore](/operations/backup-restore/) tooling, not a raw `cp` while the server runs.
- **TLS** — terminate HTTPS at your reverse proxy for the S3 port.
- **Updates** — pin an image tag (`ghcr.io/sowahq/hangar:v0.9.1`) and bump deliberately; `latest` moves.
- **Monitoring** — enable `[metrics]` in the config if you run Prometheus; the endpoint gets its own port so you can firewall it.
