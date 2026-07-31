# Newsletter Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a single-binary Go newsletter service with public subscription flows, SQL persistence, static file hosting, SMTP delivery, file-based campaign ingestion, Docker Compose deployment, and Traefik labels.

**Architecture:** One Go process runs an HTTP server and background workers. Durable state is stored behind a repository interface backed by SQL, using PostgreSQL in production and SQLite in tests/local use. Newsletter HTML files are parsed into campaigns by content SHA-256, then delivered through a rate-limited SMTP sender loop.

**Tech Stack:** Go 1.23, standard `net/http`, `database/sql`, `log/slog`, `github.com/jmoiron/sqlx`, `github.com/lib/pq`, `modernc.org/sqlite`, `github.com/pressly/goose/v3`, `github.com/yuin/goldmark-meta` is not used because newsletters are HTML with simple YAML-like front matter parsed locally.

## Global Constraints

- Configuration is read only from environment variables at startup.
- The service manages one newsletter list only.
- Public subscribe/unsubscribe flows use email validation, double opt-in confirmation tokens, token-confirmed unsubscribe, and per-IP rate limiting.
- Tokens are stored hashed, not plaintext.
- Newsletter source files are `.html` files with optional `subject` and `from_name` front matter.
- Each unique newsletter file content SHA-256 creates at most one campaign.
- Sending uses SMTP only.
- Newsletter deliveries are queued in SQL and sent one at a time with configurable `SEND_DELAY`.
- PostgreSQL is the production database; SQLite is supported for tests and local use.
- There is no admin UI, no CAPTCHA, no Redis/external queue, and no metrics endpoint in v1.
- Docker Compose deployment includes the app, PostgreSQL, Traefik labels, and mounted static/newsletter directories.

---

## File Structure

- `go.mod`, `go.sum`: Go module and dependencies.
- `cmd/newsletter/main.go`: process entrypoint, config load, DB open/migrate, HTTP server, worker startup, graceful shutdown.
- `internal/config/config.go`: env parsing and validation.
- `internal/emailaddr/emailaddr.go`: email normalization and validation.
- `internal/token/token.go`: secure token generation and SHA-256 hashing.
- `internal/store/models.go`: shared domain structs and status constants.
- `internal/store/store.go`: repository interface.
- `internal/store/sqlstore.go`: SQL repository implementation.
- `internal/store/migrations/001_init.sql`: schema for PostgreSQL and SQLite-compatible SQL.
- `internal/mailer/message.go`: email message types and helper builders.
- `internal/mailer/smtp.go`: SMTP sender implementation.
- `internal/newsletter/parser.go`: front matter, subject fallback, and hash parsing.
- `internal/worker/scanner.go`: campaign scanner and delivery row creation.
- `internal/worker/sender.go`: delivery claim/send/retry loop.
- `internal/httpserver/server.go`: route setup, dependencies, static file mounting.
- `internal/httpserver/handlers.go`: subscribe, confirm, unsubscribe, health handlers.
- `internal/ratelimit/ratelimit.go`: simple in-memory per-IP limiter.
- `public/index.html`, `public/subscribe-success.html`, `public/unsubscribe-success.html`, `public/token-error.html`, `public/assets/site.css`: default static pages.
- `Dockerfile`: multi-stage build for the Go binary.
- `docker-compose.yml`: app and PostgreSQL services with Traefik labels.
- `.env.example`: documented environment values.
- `README.md`: local run and deployment notes.

---

### Task 1: Go Module, Configuration, Email Validation, And Tokens

**Files:**

- Create: `go.mod`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/emailaddr/emailaddr.go`
- Create: `internal/emailaddr/emailaddr_test.go`
- Create: `internal/token/token.go`
- Create: `internal/token/token_test.go`

**Interfaces:**

- Produces: `config.Load() (config.Config, error)`
- Produces: `emailaddr.Normalize(raw string) (string, error)`
- Produces: `token.New() (plain string, hash string, err error)`
- Produces: `token.Hash(plain string) string`

- [ ] **Step 1: Initialize the Go module**

Run:

```bash
go mod init github.com/mustafmst/newsletter
```

- [ ] **Step 2: Write failing config tests**

Create `internal/config/config_test.go`:

```go
package config

import (
 "testing"
 "time"
)

func TestLoadRequiresCoreEnv(t *testing.T) {
 t.Setenv("HTTP_ADDR", ":8080")
 _, err := Load()
 if err == nil {
  t.Fatal("expected missing required env error")
 }
}

func TestLoadParsesValidEnv(t *testing.T) {
 t.Setenv("HTTP_ADDR", ":8080")
 t.Setenv("PUBLIC_BASE_URL", "https://newsletter.example.com")
 t.Setenv("NEWSLETTER_NAME", "Example")
 t.Setenv("DATABASE_URL", "sqlite://:memory:")
 t.Setenv("SMTP_HOST", "smtp.example.com")
 t.Setenv("SMTP_PORT", "587")
 t.Setenv("SMTP_USERNAME", "user")
 t.Setenv("SMTP_PASSWORD", "pass")
 t.Setenv("SMTP_FROM_EMAIL", "news@example.com")
 t.Setenv("SMTP_FROM_NAME", "Example News")
 t.Setenv("SMTP_TLS_MODE", "starttls")
 t.Setenv("PUBLIC_DIR", "./public")
 t.Setenv("ASSETS_DIR", "./public/assets")
 t.Setenv("SUBSCRIBE_SUCCESS_PAGE", "subscribe-success.html")
 t.Setenv("UNSUBSCRIBE_SUCCESS_PAGE", "unsubscribe-success.html")
 t.Setenv("TOKEN_ERROR_PAGE", "token-error.html")
 t.Setenv("NEWSLETTER_DIR", "./newsletters")
 t.Setenv("NEWSLETTER_SCAN_INTERVAL", "10s")
 t.Setenv("SEND_DELAY", "250ms")
 t.Setenv("MAX_SEND_ATTEMPTS", "3")
 t.Setenv("TOKEN_TTL", "24h")
 t.Setenv("RATE_LIMIT_PER_MINUTE", "5")

 cfg, err := Load()
 if err != nil {
  t.Fatalf("Load() error = %v", err)
 }
 if cfg.HTTPAddr != ":8080" {
  t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
 }
 if cfg.SMTPPort != 587 {
  t.Fatalf("SMTPPort = %d", cfg.SMTPPort)
 }
 if cfg.SendDelay != 250*time.Millisecond {
  t.Fatalf("SendDelay = %s", cfg.SendDelay)
 }
}
```

- [ ] **Step 3: Implement config loading**

Create `internal/config/config.go` with `Config` fields matching every env var in the spec, helper functions `required`, `parseInt`, and `parseDuration`, and validation that `SMTP_TLS_MODE` is one of `starttls`, `tls`, or `none`.

- [ ] **Step 4: Run config tests**

Run:

```bash
go test ./internal/config
```

Expected: PASS.

- [ ] **Step 5: Write failing email validation tests**

Create `internal/emailaddr/emailaddr_test.go`:

```go
package emailaddr

import "testing"

func TestNormalizeValidEmail(t *testing.T) {
 got, err := Normalize("  USER@Example.COM ")
 if err != nil {
  t.Fatalf("Normalize() error = %v", err)
 }
 if got != "user@example.com" {
  t.Fatalf("Normalize() = %q", got)
 }
}

func TestNormalizeRejectsInvalidEmail(t *testing.T) {
 for _, raw := range []string{"", "missing-at", "a@", "@example.com", "a b@example.com"} {
  if _, err := Normalize(raw); err == nil {
   t.Fatalf("Normalize(%q) expected error", raw)
  }
 }
}
```

- [ ] **Step 6: Implement email validation**

Create `internal/emailaddr/emailaddr.go` using `net/mail.ParseAddress`, trimming whitespace, rejecting display names, lowercasing the address, and requiring one `@` with non-empty local and domain parts.

- [ ] **Step 7: Run email tests**

Run:

```bash
go test ./internal/emailaddr
```

Expected: PASS.

- [ ] **Step 8: Write failing token tests**

Create `internal/token/token_test.go`:

```go
package token

import "testing"

func TestNewReturnsPlainAndHash(t *testing.T) {
 plain, hash, err := New()
 if err != nil {
  t.Fatalf("New() error = %v", err)
 }
 if plain == "" || hash == "" {
  t.Fatal("plain and hash must be populated")
 }
 if plain == hash {
  t.Fatal("hash must not equal plain token")
 }
 if Hash(plain) != hash {
  t.Fatal("Hash(plain) must match returned hash")
 }
}
```

- [ ] **Step 9: Implement token generation**

Create `internal/token/token.go` using 32 random bytes from `crypto/rand`, URL-safe base64 without padding for the plain token, and hex SHA-256 for the hash.

- [ ] **Step 10: Run package tests and commit**

Run:

```bash
go test ./internal/config ./internal/emailaddr ./internal/token
git add go.mod internal/config internal/emailaddr internal/token
git commit -m "feat: add core config and validation"
```

Expected: tests PASS and commit succeeds.

---

### Task 2: SQL Store And Migrations

**Files:**

- Create: `internal/store/models.go`
- Create: `internal/store/store.go`
- Create: `internal/store/sqlstore.go`
- Create: `internal/store/sqlstore_test.go`
- Create: `internal/store/migrations/001_init.sql`

**Interfaces:**

- Consumes: normalized emails and token hashes from Task 1.
- Produces: `store.Store` interface with subscriber, token, campaign, and delivery methods.
- Produces: `store.Open(ctx context.Context, databaseURL string) (*SQLStore, error)`
- Produces: `(*SQLStore).Migrate(ctx context.Context) error`

- [ ] **Step 1: Add database dependencies**

Run:

```bash
go get github.com/jmoiron/sqlx github.com/lib/pq modernc.org/sqlite github.com/pressly/goose/v3
```

- [ ] **Step 2: Write failing store tests**

Create `internal/store/sqlstore_test.go` with tests named:

```go
func TestSubscriberLifecycle(t *testing.T) {}
func TestTokenLifecycle(t *testing.T) {}
func TestCampaignHashIsUnique(t *testing.T) {}
func TestCreateDeliveriesForActiveSubscribers(t *testing.T) {}
func TestClaimPendingDeliverySkipsRetryExhausted(t *testing.T) {}
```

Each test opens `sqlite://:memory:`, calls `Migrate`, and asserts the relevant behavior through repository methods rather than direct SQL.

- [ ] **Step 3: Define models and interface**

Create:

```go
type SubscriberStatus string
const (
 SubscriberPending SubscriberStatus = "pending"
 SubscriberActive SubscriberStatus = "active"
 SubscriberUnsubscribed SubscriberStatus = "unsubscribed"
)

type TokenPurpose string
const (
 TokenConfirmSubscribe TokenPurpose = "confirm_subscribe"
 TokenConfirmUnsubscribe TokenPurpose = "confirm_unsubscribe"
)

type DeliveryStatus string
const (
 DeliveryPending DeliveryStatus = "pending"
 DeliverySent DeliveryStatus = "sent"
 DeliveryFailed DeliveryStatus = "failed"
)
```

`Store` must include `UpsertPendingSubscriber`, `ActivateSubscriberByToken`, `CreateToken`, `UseToken`, `FindSubscriberByEmail`, `CreateCampaignIfNew`, `CreateDeliveriesForCampaign`, `ClaimPendingDelivery`, `MarkDeliverySent`, `MarkDeliveryFailed`, and `Ping`.

- [ ] **Step 4: Add migration SQL**

Create `001_init.sql` with tables from the design. Use portable SQL types: `INTEGER PRIMARY KEY`, `TEXT NOT NULL`, nullable timestamp text columns, unique constraints for `subscribers.email`, `campaigns.content_sha256`, and `(campaign_id, subscriber_id)`.

- [ ] **Step 5: Implement SQL store**

Implement `sqlstore.go` with `sqlx`, driver selection based on `DATABASE_URL` prefixes `postgres://`, `postgresql://`, and `sqlite://`. Use transactions for token-based activation/unsubscribe and delivery creation.

- [ ] **Step 6: Run store tests and commit**

Run:

```bash
go test ./internal/store
git add go.mod go.sum internal/store
git commit -m "feat: add sql store"
```

Expected: tests PASS and commit succeeds.

---

### Task 3: Newsletter Parser

**Files:**

- Create: `internal/newsletter/parser.go`
- Create: `internal/newsletter/parser_test.go`

**Interfaces:**

- Produces: `newsletter.ParseFile(path string, defaultFromName string) (newsletter.Parsed, error)`
- Produces: `type Parsed struct { Path string; SHA256 string; Subject string; FromName string; HTML []byte }`

- [ ] **Step 1: Write parser tests**

Create tests:

```go
func TestParseFileUsesFrontMatterSubject(t *testing.T) {}
func TestParseFileFallsBackToTitle(t *testing.T) {}
func TestParseFileFallsBackToFilename(t *testing.T) {}
func TestParseFileHashesFullOriginalContents(t *testing.T) {}
```

Use `t.TempDir()` and write `.html` files with `os.WriteFile`.

- [ ] **Step 2: Run parser tests and verify failure**

Run:

```bash
go test ./internal/newsletter
```

Expected: FAIL because `ParseFile` does not exist.

- [ ] **Step 3: Implement parser**

Implement a local parser that:

- reads the full file into bytes
- hashes the full original bytes with SHA-256
- recognizes front matter only when the file starts with `---\n`
- parses `subject:` and `from_name:` lines until the closing `---`
- strips front matter from the returned HTML body
- extracts the first `<title>...</title>` case-insensitively when no subject is provided
- falls back to the base filename without extension
- falls back `FromName` to the provided default

- [ ] **Step 4: Run parser tests and commit**

Run:

```bash
go test ./internal/newsletter
git add internal/newsletter
git commit -m "feat: parse newsletter files"
```

Expected: tests PASS and commit succeeds.

---

### Task 4: Mailer Interface And SMTP Sender

**Files:**

- Create: `internal/mailer/message.go`
- Create: `internal/mailer/smtp.go`
- Create: `internal/mailer/message_test.go`
- Create: `internal/mailer/smtp_test.go`

**Interfaces:**

- Produces: `type Sender interface { Send(ctx context.Context, msg Message) error }`
- Produces: `type Message struct { FromEmail, FromName, ToEmail, Subject string; HTML []byte }`
- Produces: `NewConfirmationMessage`, `NewUnsubscribeMessage`, and `NewNewsletterMessage`
- Produces: `mailer.NewSMTPSender(config.Config) (*SMTPSender, error)`

- [ ] **Step 1: Write message-building tests**

Assert confirmation/unsubscribe bodies include `PUBLIC_BASE_URL` token links and newsletter messages preserve subject and HTML body.

- [ ] **Step 2: Implement message helpers**

Use simple HTML bodies for confirmation and unsubscribe emails. Escape interpolated values with `html/template.HTMLEscapeString`.

- [ ] **Step 3: Write SMTP configuration tests**

Test that invalid TLS mode is rejected by config in Task 1 and that `NewSMTPSender` accepts `starttls`, `tls`, and `none`.

- [ ] **Step 4: Implement SMTP sender**

Use `net/smtp`, construct MIME headers for HTML email, support:

- `none`: plain `smtp.SendMail`
- `starttls`: connect, issue STARTTLS, authenticate, send
- `tls`: `tls.Dial`, then SMTP over TLS

- [ ] **Step 5: Run mailer tests and commit**

Run:

```bash
go test ./internal/mailer ./internal/config
git add internal/mailer internal/config go.mod go.sum
git commit -m "feat: add smtp mailer"
```

Expected: tests PASS and commit succeeds.

---

### Task 5: Rate Limiter And HTTP Subscription Flows

**Files:**

- Create: `internal/ratelimit/ratelimit.go`
- Create: `internal/ratelimit/ratelimit_test.go`
- Create: `internal/httpserver/server.go`
- Create: `internal/httpserver/handlers.go`
- Create: `internal/httpserver/handlers_test.go`

**Interfaces:**

- Consumes: `store.Store`, `mailer.Sender`, `emailaddr.Normalize`, `token.New`, and `token.Hash`.
- Produces: `httpserver.New(deps httpserver.Dependencies) http.Handler`
- Produces: `type Dependencies struct { Store store.Store; Sender mailer.Sender; Config config.Config; Logger *slog.Logger; Clock func() time.Time }`

- [ ] **Step 1: Write rate limiter tests**

Test that `Allow("ip")` returns true for the first `limit` calls in a one-minute window, false after that, and true again after the clock advances.

- [ ] **Step 2: Implement in-memory limiter**

Implement `ratelimit.New(limit int, now func() time.Time) *Limiter` with a mutex-protected map keyed by IP and minute bucket.

- [ ] **Step 3: Write HTTP handler tests with fakes**

Create fake store and fake sender types in `handlers_test.go`. Cover:

- `POST /subscribe` rejects invalid email with `400`
- `POST /subscribe` creates pending subscriber, stores token hash, sends confirmation, and returns `202`
- `GET /confirm?token=plain` hashes the token and activates the subscriber
- `POST /unsubscribe` returns `202` without revealing whether the subscriber exists
- `GET /unsubscribe/confirm?token=plain` marks unsubscribe token used and unsubscribes
- `GET /healthz` returns `200` when store ping succeeds

- [ ] **Step 4: Implement routes and handlers**

Use `http.NewServeMux`. Parse form data with `r.ParseForm()`, use `r.RemoteAddr` plus `X-Forwarded-For` first IP for rate-limit keys, and return plain fallback responses for token success/error when page files are absent.

- [ ] **Step 5: Add static file serving**

Mount `PUBLIC_DIR` at `/` and `ASSETS_DIR` at `/assets/`. Register API routes before the root static handler so endpoint paths win.

- [ ] **Step 6: Run HTTP tests and commit**

Run:

```bash
go test ./internal/ratelimit ./internal/httpserver
git add internal/ratelimit internal/httpserver
git commit -m "feat: add subscription http flows"
```

Expected: tests PASS and commit succeeds.

---

### Task 6: Campaign Scanner And Delivery Sender Workers

**Files:**

- Create: `internal/worker/scanner.go`
- Create: `internal/worker/scanner_test.go`
- Create: `internal/worker/sender.go`
- Create: `internal/worker/sender_test.go`

**Interfaces:**

- Consumes: `newsletter.ParseFile`, `store.Store`, and `mailer.Sender`.
- Produces: `worker.ScanOnce(ctx context.Context, st store.Store, dir string, defaultFromName string, logger *slog.Logger) error`
- Produces: `worker.RunScanner(ctx context.Context, interval time.Duration, scan func(context.Context) error, logger *slog.Logger)`
- Produces: `worker.SendOne(ctx context.Context, st store.Store, sender mailer.Sender, delay time.Duration, maxAttempts int, logger *slog.Logger) (bool, error)`
- Produces: `worker.RunSender(ctx context.Context, interval time.Duration, sendOne func(context.Context) (bool, error), logger *slog.Logger)`

- [ ] **Step 1: Write scanner tests**

Using SQLite store and temp files, assert:

- a new `.html` file creates one campaign
- active subscribers get pending delivery rows
- the same content hash is skipped on the second scan
- non-HTML files are ignored

- [ ] **Step 2: Implement scanner**

Use `filepath.WalkDir`, skip directories and non-`.html` files, parse files through `newsletter.ParseFile`, call `CreateCampaignIfNew`, then `CreateDeliveriesForCampaign` only when the campaign was newly inserted.

- [ ] **Step 3: Write sender tests**

Using a fake sender and SQLite store, assert:

- one pending delivery is sent and marked `sent`
- a sender error increments attempt count and stores `last_error`
- a delivery at `MAX_SEND_ATTEMPTS` becomes `failed`

- [ ] **Step 4: Implement sender**

`SendOne` claims one pending delivery, builds a newsletter message from campaign and delivery data, sends it, records success/failure, and sleeps for `delay` only after an actual send attempt. `RunSender` loops until context cancellation.

- [ ] **Step 5: Run worker tests and commit**

Run:

```bash
go test ./internal/worker ./internal/store ./internal/newsletter
git add internal/worker internal/store
git commit -m "feat: add newsletter workers"
```

Expected: tests PASS and commit succeeds.

---

### Task 7: Application Entrypoint And Graceful Runtime

**Files:**

- Create: `cmd/newsletter/main.go`
- Modify: `internal/store/sqlstore.go`
- Modify: `internal/config/config.go`

**Interfaces:**

- Consumes: `config.Load`, `store.Open`, `SQLStore.Migrate`, `httpserver.New`, `worker.RunScanner`, `worker.RunSender`, and `mailer.NewSMTPSender`.
- Produces: runnable command `go run ./cmd/newsletter`.

- [ ] **Step 1: Write startup smoke test script command**

Use this command during implementation:

```bash
go run ./cmd/newsletter
```

Expected before required env vars are set: exits non-zero and logs missing configuration without printing secrets.

- [ ] **Step 2: Implement main**

Main must:

- load config
- configure `slog` JSON logger
- open and migrate DB
- create SMTP sender
- create HTTP handler
- start scanner and sender goroutines with a cancellable context
- start `http.Server`
- shut down on `SIGINT` or `SIGTERM` using `http.Server.Shutdown`

- [ ] **Step 3: Verify missing-env behavior**

Run:

```bash
go run ./cmd/newsletter
```

Expected: non-zero exit with a clear missing env error.

- [ ] **Step 4: Verify local SQLite startup**

Run with minimal env:

```bash
HTTP_ADDR=:8080 PUBLIC_BASE_URL=http://localhost:8080 NEWSLETTER_NAME=Local DATABASE_URL=sqlite://newsletter.db SMTP_HOST=localhost SMTP_PORT=1025 SMTP_USERNAME=user SMTP_PASSWORD=pass SMTP_FROM_EMAIL=news@example.com SMTP_FROM_NAME=Local SMTP_TLS_MODE=none PUBLIC_DIR=./public ASSETS_DIR=./public/assets SUBSCRIBE_SUCCESS_PAGE=subscribe-success.html UNSUBSCRIBE_SUCCESS_PAGE=unsubscribe-success.html TOKEN_ERROR_PAGE=token-error.html NEWSLETTER_DIR=./newsletters NEWSLETTER_SCAN_INTERVAL=10s SEND_DELAY=1s MAX_SEND_ATTEMPTS=3 TOKEN_TTL=24h RATE_LIMIT_PER_MINUTE=5 go run ./cmd/newsletter
```

Expected: server starts after default `public` and `newsletters` directories exist in Task 8.

- [ ] **Step 5: Commit**

Run:

```bash
go test ./...
git add cmd internal
git commit -m "feat: wire application runtime"
```

Expected: tests PASS and commit succeeds.

---

### Task 8: Static Defaults, Docker, Compose, And Documentation

**Files:**

- Create: `public/index.html`
- Create: `public/subscribe-success.html`
- Create: `public/unsubscribe-success.html`
- Create: `public/token-error.html`
- Create: `public/assets/site.css`
- Create: `newsletters/.gitkeep`
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.env.example`
- Create: `README.md`

**Interfaces:**

- Consumes: env vars from `internal/config.Config`.
- Produces: local static files, production container image, compose deployment with Traefik labels.

- [ ] **Step 1: Add default static files**

Create accessible HTML pages with one subscribe form posting to `/subscribe`, one unsubscribe form posting to `/unsubscribe`, success pages, and a token error page. Keep the pages static and dependency-free except `/assets/site.css`.

- [ ] **Step 2: Add Dockerfile**

Use a multi-stage Dockerfile:

```dockerfile
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/newsletter ./cmd/newsletter

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/newsletter /app/newsletter
EXPOSE 8080
ENTRYPOINT ["/app/newsletter"]
```

- [ ] **Step 3: Add docker-compose.yml**

Compose must define `app` and `postgres`, mount `./public:/app/public:ro` and `./newsletters:/app/newsletters:ro`, set every required env var, and include Traefik labels using `${TRAEFIK_HOST}`, `${TRAEFIK_ENTRYPOINT}`, and `${TRAEFIK_CERT_RESOLVER}`.

- [ ] **Step 4: Add `.env.example`**

Include concrete sample values for every required env var, including:

```dotenv
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://newsletter.example.com
NEWSLETTER_NAME=Example Newsletter
DATABASE_URL=postgres://newsletter:newsletter@postgres:5432/newsletter?sslmode=disable
SMTP_TLS_MODE=starttls
NEWSLETTER_SCAN_INTERVAL=30s
SEND_DELAY=2s
MAX_SEND_ATTEMPTS=3
TOKEN_TTL=24h
RATE_LIMIT_PER_MINUTE=5
TRAEFIK_HOST=newsletter.example.com
TRAEFIK_ENTRYPOINT=websecure
TRAEFIK_CERT_RESOLVER=letsencrypt
```

- [ ] **Step 5: Add README**

Document:

- service purpose
- env configuration
- newsletter file format
- local SQLite run command
- Docker Compose deployment
- Traefik assumptions
- how duplicate campaign detection works

- [ ] **Step 6: Verify all tests and build**

Run:

```bash
go test ./...
go build ./cmd/newsletter
```

Expected: both commands PASS.

- [ ] **Step 7: Commit**

Run:

```bash
git add public newsletters Dockerfile docker-compose.yml .env.example README.md
git commit -m "chore: add deployment assets"
```

Expected: commit succeeds.

---

### Task 9: End-To-End Local Verification

**Files:**

- Modify only when verification reveals a defect in files from earlier tasks.

**Interfaces:**

- Consumes: the complete application from Tasks 1-8.
- Produces: verified service behavior against SQLite and fake/local SMTP.

- [ ] **Step 1: Run full test suite**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run full build**

Run:

```bash
go build ./cmd/newsletter
```

Expected: PASS.

- [ ] **Step 3: Start local SMTP capture**

If `mailpit` is available, run:

```bash
mailpit --smtp :1025 --listen :8025
```

If `mailpit` is not available, use tests as SMTP verification and document that live SMTP smoke testing was skipped.

- [ ] **Step 4: Start service locally**

Run:

```bash
HTTP_ADDR=:8080 PUBLIC_BASE_URL=http://localhost:8080 NEWSLETTER_NAME=Local DATABASE_URL=sqlite://newsletter.db SMTP_HOST=localhost SMTP_PORT=1025 SMTP_USERNAME=user SMTP_PASSWORD=pass SMTP_FROM_EMAIL=news@example.com SMTP_FROM_NAME=Local SMTP_TLS_MODE=none PUBLIC_DIR=./public ASSETS_DIR=./public/assets SUBSCRIBE_SUCCESS_PAGE=subscribe-success.html UNSUBSCRIBE_SUCCESS_PAGE=unsubscribe-success.html TOKEN_ERROR_PAGE=token-error.html NEWSLETTER_DIR=./newsletters NEWSLETTER_SCAN_INTERVAL=5s SEND_DELAY=250ms MAX_SEND_ATTEMPTS=3 TOKEN_TTL=24h RATE_LIMIT_PER_MINUTE=5 go run ./cmd/newsletter
```

Expected: server listens on `:8080`.

- [ ] **Step 5: Verify public endpoints**

Run:

```bash
curl -i http://localhost:8080/healthz
curl -i -X POST -d 'email=user@example.com' http://localhost:8080/subscribe
curl -i -X POST -d 'email=user@example.com' http://localhost:8080/unsubscribe
```

Expected: health returns `200`; subscribe and unsubscribe requests return `202`.

- [ ] **Step 6: Verify campaign ingestion**

Create `newsletters/smoke.html`:

```html
---
subject: Smoke Test
---

<html>
  <head>
    <title>Fallback</title>
  </head>
  <body>
    <p>Hello.</p>
  </body>
</html>
```

Wait for one scan interval, then inspect logs for campaign creation. Repeat with unchanged contents and verify logs show a duplicate hash skip rather than a second campaign.

- [ ] **Step 7: Commit verification fixes**

If fixes were required, run:

```bash
go test ./...
git add .
git commit -m "fix: address local verification issues"
```

Expected: commit exists only if a defect was fixed.

---

## Self-Review Notes

- Spec coverage: subscription endpoints, token flows, static hosting, SQL persistence, newsletter parsing, hash-based campaign idempotence, SMTP sending, delayed queueing, Docker Compose, Traefik labels, and tests are all covered by tasks.
- Red-flag scan: no unresolved markers are intentionally left in this plan.
- Type consistency: exported functions consumed by later tasks are defined in earlier task interface sections.
