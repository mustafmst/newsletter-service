# Newsletter Production Hardening Design

## Context

The newsletter service is a single Go binary deployed with Docker Compose behind Traefik. It handles public subscribe/unsubscribe flows, sends confirmation and newsletter email through SMTP, scans a mounted newsletter directory, and stores durable state in PostgreSQL for production.

A production-readiness review found that the service is close to deployable for a small single-instance setup, but it needs runtime and deployment hardening before being exposed to the internet.

This pass targets one app instance behind Traefik. It does not aim to support horizontal scaling or distributed workers.

## Goals

- Prevent SMTP/network stalls from hanging HTTP handlers or the sender loop indefinitely.
- Add complete HTTP server timeouts.
- Validate configuration values that can panic the process or silently disable core behavior.
- Keep lightweight in-memory rate limiting, but make proxy-header trust explicit and bound limiter memory growth.
- Tighten one-time token use so concurrent requests cannot both consume the same token.
- Polish Docker Compose and sample environment files so production secrets are not hard-coded.
- Add focused tests for the hardening behavior.
- Keep the architecture simple and avoid adding external services.

## Non-Goals

- No multi-instance delivery coordination.
- No Redis, external queue, or distributed rate limiter.
- No admin UI.
- No metrics system.
- No CAPTCHA or third-party abuse-prevention integration.
- No broad database migration framework in this pass.

## Architecture

The service remains one process with:

- HTTP server for public endpoints, static pages, and health checks.
- Scanner worker for newsletter files.
- Sender worker for queued newsletter deliveries.
- SQL repository for subscribers, tokens, campaigns, and deliveries.
- SMTP sender implementation behind the existing `mailer.Sender` interface.

The hardening changes stay within existing package boundaries:

- `internal/config`: parse and validate new and existing production settings.
- `cmd/newsletter`: wire HTTP timeouts and pass config to dependencies.
- `internal/mailer`: apply SMTP operation deadlines.
- `internal/httpserver` and `internal/ratelimit`: handle proxy-aware client identity and bounded limiter storage.
- `internal/store`: make token consumption atomic.
- deployment files and README: clarify production configuration.

## Configuration

Existing required values remain required. Validation will reject:

- `SMTP_PORT` outside `1..65535`.
- `NEWSLETTER_SCAN_INTERVAL <= 0`.
- `SEND_DELAY < 0`.
- `MAX_SEND_ATTEMPTS < 1`.
- `TOKEN_TTL <= 0`.
- `RATE_LIMIT_PER_MINUTE < 0`.

`RATE_LIMIT_PER_MINUTE=0` means rate limiting is intentionally disabled.

Add:

- `TRUST_PROXY_HEADERS`: boolean. When true, client IP detection uses `X-Forwarded-For` when present. When false, rate limiting uses `RemoteAddr`.
- `SMTP_TIMEOUT`: positive duration used as the per-send network deadline.

The sample `.env.example` will set `TRUST_PROXY_HEADERS=true` for the Traefik Compose deployment, set `SMTP_TIMEOUT=30s`, and use obvious placeholder secrets instead of production-looking defaults.

## HTTP Runtime

The HTTP server will use:

- `ReadHeaderTimeout`: 10 seconds.
- `ReadTimeout`: 15 seconds.
- `WriteTimeout`: 30 seconds.
- `IdleTimeout`: 60 seconds.

These fixed values are long enough for small form posts and static files, while preventing idle or slow clients from tying up server resources.

Request logging remains structured and must not include tokens, submitted email addresses, or SMTP secrets.

## SMTP Runtime

SMTP sends will use `SMTP_TIMEOUT` as a deadline for network operations. The implementation should avoid indefinitely blocking in:

- TCP dial.
- TLS handshake.
- SMTP command exchange.
- Message data write.

For `starttls` and `tls`, TLS keeps `MinVersion: tls.VersionTLS12` and `ServerName` set to the configured SMTP host.

Subscribe and unsubscribe confirmation emails will remain synchronous in this pass, because the single-instance scope does not require a separate confirmation queue. The timeout ensures those handlers fail rather than hang indefinitely when SMTP is unavailable.

## Rate Limiting

The limiter remains in-memory and per-process.

Client identity rules:

- When `TRUST_PROXY_HEADERS=false`, use `RemoteAddr` only.
- When `TRUST_PROXY_HEADERS=true`, use the first non-empty IP from `X-Forwarded-For`, falling back to `RemoteAddr`.

The limiter will prune buckets from older minutes so arbitrary client keys cannot grow memory without bound. This is still a lightweight abuse control, not a security boundary against large distributed attacks.

Deployment documentation will state that proxy header trust is safe only when the app service is not directly exposed to the public internet and Traefik controls inbound headers.

## Token Use

Token storage remains hashed.

Token consumption should become atomic by updating `used_at` only when:

- `hash` matches.
- `purpose` matches.
- `used_at IS NULL`.
- `expires_at` is still in the future.

The code then checks affected rows. If no row was updated, the token is treated as missing, expired, or already used. This prevents two concurrent confirmation requests from both successfully consuming the same token.

The subscriber status update remains in the same transaction as token consumption.

## Delivery Worker

The runtime remains a single sender goroutine in one app instance.

`ClaimPendingDelivery` will stay as a simple pending-row selection for this pass, because no second sender exists in the target deployment. The implementation will not introduce a larger delivery state machine.

The README should document that running multiple app instances against the same database is outside this version's supported deployment model.

## Deployment

Docker remains multi-stage with a distroless runtime.

Compose changes:

- Use environment interpolation for the Postgres password instead of hard-coding `newsletter`.
- Keep the app service reachable by Traefik on the Docker network.
- Avoid publishing the app port directly.
- Do not add an app container healthcheck in this pass because the distroless runtime image does not include a shell, curl, or wget. Document using Traefik or external health checks against `/healthz`.

The README will include production notes:

- Change all placeholder secrets.
- Keep `DATABASE_URL` aligned with the Compose Postgres credentials.
- Set `TRUST_PROXY_HEADERS=true` only behind Traefik or another trusted proxy.
- Do not run multiple app replicas for this version.
- Prefer `SMTP_TLS_MODE=starttls` or `tls`; use `none` only for local development.

## Error Handling

- Startup fails fast on invalid config.
- SMTP timeout errors are logged and surfaced as handler failures for confirmation mail.
- Newsletter delivery timeout errors are persisted in `deliveries.last_error` and retried according to `MAX_SEND_ATTEMPTS`.
- Rate-limit rejections continue returning `429`.
- Invalid, expired, or reused tokens continue serving the configured token error page or fallback response.

## Testing

Add or update focused tests for:

- Config validation rejects unsafe values and parses new settings.
- HTTP server wiring uses the expected timeout values.
- Client IP detection ignores `X-Forwarded-For` unless proxy trust is enabled.
- Rate limiter prunes stale client buckets.
- Token consumption rejects a second use of the same token, including an affected-row check.
- SMTP sender stores the configured timeout and applies it through testable dial/deadline seams.

Existing tests must continue passing.

## Acceptance Criteria

- `go test ./...` passes.
- `go vet ./...` passes.
- Invalid production-impacting environment values fail startup with clear errors.
- SMTP operations cannot block indefinitely under normal network timeout behavior.
- HTTP server has read, write, idle, and header timeouts configured.
- Public endpoint rate limiting cannot be bypassed by spoofed `X-Forwarded-For` unless proxy trust is explicitly enabled.
- Rate limiter memory does not grow forever from old client keys.
- A token cannot be successfully consumed twice.
- Compose and `.env.example` no longer present `newsletter` as a production-looking database password.
- README documents single-instance deployment assumptions and production configuration notes.
