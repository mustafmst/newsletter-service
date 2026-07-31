# Newsletter Service Design

## Context

Build a simple newsletter service in Go. The service is configured entirely through environment variables, runs as a single deployable binary, and is intended for Docker Compose deployment behind Traefik. It has no admin UI.

The service manages one newsletter list, supports double opt-in subscribe and token-confirmed unsubscribe flows, serves static pages/assets from configured directories, and sends newsletters from HTML files placed in a configured directory.

## Goals

- Provide public endpoints for subscribe, confirm subscription, request unsubscribe, and confirm unsubscribe.
- Validate email addresses and protect public endpoints with lightweight per-IP rate limiting.
- Send confirmation, unsubscribe, and newsletter emails through SMTP.
- Watch or scan a configured directory for new newsletter HTML files.
- Treat each unique newsletter file content hash as one campaign.
- Queue newsletter delivery and send messages gradually using a configurable delay.
- Store subscribers, tokens, campaigns, processed file hashes, and delivery attempts in SQL.
- Support PostgreSQL for production and SQLite for local development and tests.
- Provide Docker and Docker Compose assets suitable for Traefik-based deployment.

## Non-Goals

- No admin UI.
- No multi-list support.
- No built-in newsletter editor.
- No CAPTCHA or third-party anti-abuse service.
- No external queue such as Redis in the first version.
- No metrics endpoint in the first version.

## Architecture

The application is one Go binary with two main parts running in the same process:

- HTTP server: handles public subscription endpoints, confirmation and unsubscribe links, static page and asset hosting, and health checks.
- Background mailer: scans the newsletter directory, parses new campaign files, creates delivery records, and sends queued emails with delay and retry limits.

All durable state is stored in SQL through a small repository layer. The repository supports PostgreSQL for production and SQLite for tests and local use.

## HTTP And Static Pages

Public endpoints:

- `POST /subscribe`: accepts an email address, validates syntax, applies per-IP rate limiting, creates or refreshes a pending subscriber, and sends a subscription confirmation email.
- `GET /confirm?token=...`: validates the token, activates the subscriber, marks the token used, and serves the configured subscription success page or a plain fallback response.
- `POST /unsubscribe`: accepts an email address, validates syntax, applies per-IP rate limiting, and sends an unsubscribe confirmation email when the address exists.
- `GET /unsubscribe/confirm?token=...`: validates the token, marks the subscriber unsubscribed, marks the token used, and serves the configured unsubscribe success page or a plain fallback response.
- `GET /healthz`: returns healthy only when the process is running and the database is reachable.

Static files are served from configured directories such as `PUBLIC_DIR` and `ASSETS_DIR`. The service does not need a template renderer in the first version. Static HTML pages can contain normal forms that post to the public endpoints.

Confirmation and unsubscribe emails use links based on `PUBLIC_BASE_URL`.

## Data Model

Core tables:

- `subscribers`: email, status, created timestamp, updated timestamp, confirmed timestamp, and unsubscribed timestamp.
- `tokens`: hashed token value, purpose, subscriber id, expiry timestamp, created timestamp, and used timestamp.
- `campaigns`: source file path, content SHA-256, subject, from name, status, created timestamp, and processed timestamp.
- `deliveries`: campaign id, subscriber id, recipient email snapshot, status, attempt count, last error, created timestamp, updated timestamp, and sent timestamp.
- `schema_migrations`: migration tracking table if the selected migration approach requires it.

Subscriber status values are `pending`, `active`, and `unsubscribed`.

Token purpose values are `confirm_subscribe` and `confirm_unsubscribe`.

Delivery status values are `pending`, `sent`, and `failed`.

Constraints:

- Subscriber email is unique.
- Campaign content SHA-256 is unique.
- One delivery row exists per campaign and subscriber.
- Tokens are stored hashed, not as plaintext.

## Newsletter File Format

Newsletter files are HTML files with optional front matter:

```text
---
subject: My newsletter title
from_name: Example Newsletter
---
<html>...</html>
```

The subject is resolved in this order:

1. `subject` front matter.
2. First `<title>` element.
3. Source filename.

`from_name` falls back to the configured SMTP sender name when omitted.

## Mailer Flow

The scanner runs on `NEWSLETTER_SCAN_INTERVAL` and inspects readable `.html` files under `NEWSLETTER_DIR`.

For each file:

1. Read the file contents.
2. Compute SHA-256 over the full file contents.
3. Skip the file when a campaign with that hash already exists.
4. Parse optional front matter.
5. Resolve the subject.
6. Create a campaign row.
7. Create pending delivery rows for subscribers currently in `active` status.

The sender loop claims pending deliveries one at a time, sends through SMTP, records success or failure, then sleeps for `SEND_DELAY`. Failed sends retry until `MAX_SEND_ATTEMPTS`; after that the delivery remains `failed` with `last_error` populated.

Processed means the content hash exists in the `campaigns` table. Files can remain in the directory. Editing a file changes the hash and creates a new campaign.

## Configuration

All configuration is read from environment variables at startup.

Server:

- `HTTP_ADDR`
- `PUBLIC_BASE_URL`
- `NEWSLETTER_NAME`

Database:

- `DATABASE_URL`

SMTP:

- `SMTP_HOST`
- `SMTP_PORT`
- `SMTP_USERNAME`
- `SMTP_PASSWORD`
- `SMTP_FROM_EMAIL`
- `SMTP_FROM_NAME`
- `SMTP_TLS_MODE`: `starttls`, `tls`, or `none`

Static files:

- `PUBLIC_DIR`
- `ASSETS_DIR`
- `SUBSCRIBE_SUCCESS_PAGE`
- `UNSUBSCRIBE_SUCCESS_PAGE`
- `TOKEN_ERROR_PAGE`

Newsletter input:

- `NEWSLETTER_DIR`
- `NEWSLETTER_SCAN_INTERVAL`

Sending and abuse protection:

- `SEND_DELAY`
- `MAX_SEND_ATTEMPTS`
- `TOKEN_TTL`
- `RATE_LIMIT_PER_MINUTE`

## Deployment

The repository will include:

- `Dockerfile` for the Go binary.
- `docker-compose.yml` with app and PostgreSQL services.
- Traefik labels on the app service, driven by compose variables for hostname, entrypoint, and certificate resolver.
- Mounted volumes for static pages and newsletter input files.

Operators configure the service by editing Docker Compose environment values, mounted static files, mounted newsletter files, or database contents.

## Error Handling

- Invalid email addresses return `400`.
- Unknown, expired, or used tokens return `TOKEN_ERROR_PAGE` when configured, otherwise a clear plain response.
- Duplicate subscribe requests are idempotent and safely refresh or resend confirmation.
- Unsubscribe requests do not reveal whether an email exists.
- SMTP failures are recorded in `deliveries.last_error`.
- Bad newsletter files are logged and skipped until corrected.
- Startup fails fast when required configuration is missing or invalid.

## Observability

The service emits structured logs for:

- startup configuration summary without secrets
- HTTP requests
- rate-limit rejections
- token creation and use
- campaign creation and duplicate hash skips
- delivery success, failure, and retry exhaustion

`GET /healthz` verifies that the process is alive and the database is reachable.

## Tests

Expected test coverage:

- Email validation.
- Token creation, hashing, expiry, and use.
- Repository behavior against SQLite.
- HTTP handlers for subscribe, confirm, unsubscribe request, and unsubscribe confirmation.
- Newsletter parser behavior for front matter, `<title>` fallback, and filename fallback.
- Worker behavior for duplicate SHA detection and delivery row creation.
- SMTP sending through a fake sender interface.

## Acceptance Criteria

- A user can subscribe with a valid email, receive a confirmation link, and become active after clicking it.
- A user can request unsubscribe, receive a confirmation link, and become unsubscribed after clicking it.
- Invalid emails and abusive request rates are rejected.
- Static pages and dependencies are served from configured directories.
- A new HTML file in the newsletter directory creates one campaign and one pending delivery per active subscriber.
- Re-adding the same file contents does not create a duplicate campaign.
- Newsletter emails are sent through SMTP with a configurable delay between recipients.
- Delivery successes and failures are persisted.
- The service can run with PostgreSQL through Docker Compose and with SQLite in tests.
