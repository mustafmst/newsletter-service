# Newsletter

A simple single-list newsletter service written in Go. It handles double opt-in subscriptions, token-confirmed unsubscribes, static public pages, and SMTP newsletter delivery from HTML files placed in a watched directory.

## Configuration

All configuration is read from environment variables at startup. Copy `.env.example` to `.env` for Docker Compose deployment and adjust the values.

Required groups:

- Server: `HTTP_ADDR`, `PUBLIC_BASE_URL`, `NEWSLETTER_NAME`
- Database: `DATABASE_URL`
- SMTP: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_EMAIL`, `SMTP_FROM_NAME`, `SMTP_TLS_MODE`, `SMTP_TIMEOUT`
- Static files: `PUBLIC_DIR`, `ASSETS_DIR`, `SUBSCRIBE_SUCCESS_PAGE`, `UNSUBSCRIBE_SUCCESS_PAGE`, `TOKEN_ERROR_PAGE`
- Newsletter input: `NEWSLETTER_DIR`, `NEWSLETTER_SCAN_INTERVAL`
- Sending and abuse protection: `SEND_DELAY`, `MAX_SEND_ATTEMPTS`, `TOKEN_TTL`, `RATE_LIMIT_PER_MINUTE`, `TRUST_PROXY_HEADERS`

`SMTP_TLS_MODE` must be `starttls`, `tls`, or `none`.
`SMTP_TIMEOUT` is one deadline for the complete SMTP send and must be positive and less than the fixed 30-second HTTP write timeout. The sample 20-second value reserves time for synchronous confirmation handlers to return an HTTP response.

## Newsletter Files

Add `.html` files to `NEWSLETTER_DIR`. Each unique file content SHA-256 becomes one campaign. The file may stay in the directory; unchanged contents are skipped on later scans. Editing the file changes the hash and creates a new campaign.

Optional front matter:

```text
---
subject: My newsletter title
from_name: Example Newsletter
---
<html>...</html>
```

Subject fallback order:

1. `subject` front matter.
2. First `<title>` element.
3. Source filename without extension.

## Local SQLite Run

Create the local directories if needed, then run:

```bash
HTTP_ADDR=:8080 PUBLIC_BASE_URL=http://localhost:8080 NEWSLETTER_NAME=Local DATABASE_URL=sqlite://newsletter.db SMTP_HOST=localhost SMTP_PORT=1025 SMTP_USERNAME=user SMTP_PASSWORD=pass SMTP_FROM_EMAIL=news@example.com SMTP_FROM_NAME=Local SMTP_TLS_MODE=none SMTP_TIMEOUT=20s TRUST_PROXY_HEADERS=false PUBLIC_DIR=./public ASSETS_DIR=./public/assets SUBSCRIBE_SUCCESS_PAGE=subscribe-success.html UNSUBSCRIBE_SUCCESS_PAGE=unsubscribe-success.html TOKEN_ERROR_PAGE=token-error.html NEWSLETTER_DIR=./newsletters NEWSLETTER_SCAN_INTERVAL=10s SEND_DELAY=1s MAX_SEND_ATTEMPTS=3 TOKEN_TTL=24h RATE_LIMIT_PER_MINUTE=5 go run ./cmd/newsletter
```

For local SMTP capture, run a tool such as Mailpit on port `1025`.

## Docker Compose

Copy `.env.example` to `.env`, update SMTP and Traefik values, then run:

```bash
docker compose up -d --build
```

The compose file starts the app and PostgreSQL. It expects Traefik to be attached to the same Docker network or otherwise able to route to this stack. Static files are mounted from `./public`, and newsletter input files are mounted from `./newsletters`.

## Production Notes

- Replace all placeholder secrets in `.env` before deployment.
- Keep `DATABASE_URL` credentials aligned with `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB`.
- This version supports a single app instance against the database. Do not run multiple app replicas for newsletter sending.
- Set `TRUST_PROXY_HEADERS=true` only when the app is reachable exclusively through Traefik or another trusted proxy that controls inbound forwarding headers.
- Do not publish the app container port directly to the public internet.
- Prefer `SMTP_TLS_MODE=starttls` or `tls` in production. Use `none` only for local SMTP capture tools.
- Keep `SMTP_TIMEOUT` below 30 seconds so synchronous confirmation requests retain time to write their HTTP response after SMTP returns.
- The app image is distroless and does not include curl, wget, or a shell. Use Traefik or an external monitor to check `GET /healthz`.

## Public Endpoints

- `POST /subscribe`
- `GET /confirm?token=...`
- `POST /unsubscribe`
- `GET /unsubscribe/confirm?token=...`
- `GET /healthz`
