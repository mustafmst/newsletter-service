# Production Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the single-instance Docker Compose newsletter service for production exposure behind Traefik.

**Architecture:** Keep the existing one-process Go service and package boundaries. Add stricter configuration validation, fixed HTTP runtime timeouts, SMTP network deadlines, explicit proxy-header trust, bounded in-memory rate limiting, atomic token consumption, and deployment documentation polish.

**Tech Stack:** Go 1.26.1, standard `net/http`, `net/smtp`, `crypto/tls`, `database/sql`, `sqlx`, PostgreSQL via `lib/pq`, SQLite via `modernc.org/sqlite`, Docker Compose, Traefik labels.

## Global Constraints

- Target deployment is one app instance behind Traefik.
- Do not add Redis, an external queue, a distributed rate limiter, an admin UI, metrics, CAPTCHA, or a broad migration framework.
- `RATE_LIMIT_PER_MINUTE=0` means rate limiting is intentionally disabled.
- `TRUST_PROXY_HEADERS=true` means client IP detection uses `X-Forwarded-For` when present.
- `TRUST_PROXY_HEADERS=false` means rate limiting uses `RemoteAddr`.
- `SMTP_TIMEOUT` is a positive duration used as the per-send network deadline.
- HTTP timeout values are fixed: `ReadHeaderTimeout=10s`, `ReadTimeout=15s`, `WriteTimeout=30s`, `IdleTimeout=60s`.
- SMTP TLS keeps `MinVersion: tls.VersionTLS12` and `ServerName` set to `SMTP_HOST`.
- Subscribe and unsubscribe confirmation emails remain synchronous.
- `ClaimPendingDelivery` remains a simple pending-row selection; do not introduce a larger delivery state machine.
- Do not add an app container healthcheck because the distroless runtime image does not include shell, curl, or wget.
- Every task must end with `go test` for the touched packages and a focused commit.

---

## File Structure

- `internal/config/config.go`: Owns environment parsing and validation. Add `TrustProxyHeaders bool`, `SMTPTimeout time.Duration`, boolean parsing, and value range checks.
- `internal/config/config_test.go`: Covers valid parsing and invalid production-impacting values.
- `cmd/newsletter/main.go`: Owns application wiring. Add a small `newHTTPServer` helper that centralizes HTTP timeout values.
- `cmd/newsletter/main_test.go`: Verifies the HTTP server helper uses the fixed timeout values.
- `internal/httpserver/server.go`: Carries `TrustProxyHeaders` from config into the server instance.
- `internal/httpserver/handlers.go`: Uses proxy-aware client IP resolution for rate limiting.
- `internal/httpserver/handlers_test.go`: Covers client IP behavior with proxy trust enabled and disabled.
- `internal/ratelimit/ratelimit.go`: Owns per-minute in-memory rate limiting. Add stale bucket pruning.
- `internal/ratelimit/ratelimit_test.go`: Covers pruning and existing limit behavior.
- `internal/mailer/smtp.go`: Owns SMTP transport behavior. Replace unbounded SMTP dialing with context-aware dialing and connection deadlines.
- `internal/mailer/smtp_test.go`: Covers timeout config storage and deadline application through fake connections/client seams.
- `internal/store/sqlstore.go`: Owns token persistence. Make token consumption update-first and atomic.
- `internal/store/sqlstore_test.go`: Covers expired/reused token behavior and the existing subscriber lifecycle.
- `.env.example`: Adds new env vars and removes production-looking database password defaults.
- `docker-compose.yml`: Interpolates Postgres password from env and keeps app unexposed except through Traefik.
- `README.md`: Documents production notes, single-instance assumption, proxy-header trust, SMTP TLS guidance, and external health checks.

---

### Task 1: Configuration Parsing And Validation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config.TrustProxyHeaders bool`
- Produces: `config.Config.SMTPTimeout time.Duration`
- Produces: `parseBool(key string) (bool, error)`
- Produces: `validate(cfg Config) error`
- Consumed by later tasks: `cfg.TrustProxyHeaders`, `cfg.SMTPTimeout`

- [ ] **Step 1: Write the failing valid-config test**

Add `TRUST_PROXY_HEADERS` and `SMTP_TIMEOUT` to `TestLoadParsesValidEnv` in `internal/config/config_test.go`:

```go
t.Setenv("TRUST_PROXY_HEADERS", "true")
t.Setenv("SMTP_TIMEOUT", "30s")
```

Add assertions after the existing `SendDelay` assertion:

```go
if !cfg.TrustProxyHeaders {
	t.Fatal("TrustProxyHeaders = false, want true")
}
if cfg.SMTPTimeout != 30*time.Second {
	t.Fatalf("SMTPTimeout = %s", cfg.SMTPTimeout)
}
```

- [ ] **Step 2: Write failing invalid-config table tests**

Add this helper and test to `internal/config/config_test.go`:

```go
func setValidEnv(t *testing.T) {
	t.Helper()
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
	t.Setenv("TRUST_PROXY_HEADERS", "false")
	t.Setenv("SMTP_TIMEOUT", "30s")
}

func TestLoadRejectsUnsafeValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
		raw  string
	}{
		{name: "smtp port zero", key: "SMTP_PORT", raw: "0"},
		{name: "smtp port too high", key: "SMTP_PORT", raw: "65536"},
		{name: "scan interval zero", key: "NEWSLETTER_SCAN_INTERVAL", raw: "0s"},
		{name: "send delay negative", key: "SEND_DELAY", raw: "-1s"},
		{name: "max attempts zero", key: "MAX_SEND_ATTEMPTS", raw: "0"},
		{name: "token ttl zero", key: "TOKEN_TTL", raw: "0s"},
		{name: "rate limit negative", key: "RATE_LIMIT_PER_MINUTE", raw: "-1"},
		{name: "smtp timeout zero", key: "SMTP_TIMEOUT", raw: "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tt.key, tt.raw)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}
}

func TestLoadParsesDisabledRateLimit(t *testing.T) {
	setValidEnv(t)
	t.Setenv("RATE_LIMIT_PER_MINUTE", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RateLimitPerMinute != 0 {
		t.Fatalf("RateLimitPerMinute = %d, want 0", cfg.RateLimitPerMinute)
	}
}
```

Then replace the repeated environment setup inside `TestLoadParsesValidEnv` with `setValidEnv(t)` so the test suite has one complete valid environment source.

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/config
```

Expected: FAIL because `Config` has no `TrustProxyHeaders` or `SMTPTimeout`, and `Load` does not parse the new env vars yet.

- [ ] **Step 4: Implement config fields, parsing, and validation**

In `internal/config/config.go`, add fields to `Config`:

```go
TrustProxyHeaders bool
SMTPTimeout       time.Duration
```

After `RATE_LIMIT_PER_MINUTE` parsing in `Load`, parse the new values and validate:

```go
if cfg.TrustProxyHeaders, err = parseBool("TRUST_PROXY_HEADERS"); err != nil {
	return Config{}, err
}
if cfg.SMTPTimeout, err = parseDuration("SMTP_TIMEOUT"); err != nil {
	return Config{}, err
}
if err := validate(cfg); err != nil {
	return Config{}, err
}
```

Add these helpers below `parseDuration`:

```go
func parseBool(key string) (bool, error) {
	raw, err := required(key)
	if err != nil {
		return false, err
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return value, nil
}

func validate(cfg Config) error {
	if cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 {
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	if cfg.NewsletterScanInterval <= 0 {
		return fmt.Errorf("NEWSLETTER_SCAN_INTERVAL must be greater than zero")
	}
	if cfg.SendDelay < 0 {
		return fmt.Errorf("SEND_DELAY must be zero or greater")
	}
	if cfg.MaxSendAttempts < 1 {
		return fmt.Errorf("MAX_SEND_ATTEMPTS must be at least 1")
	}
	if cfg.TokenTTL <= 0 {
		return fmt.Errorf("TOKEN_TTL must be greater than zero")
	}
	if cfg.RateLimitPerMinute < 0 {
		return fmt.Errorf("RATE_LIMIT_PER_MINUTE must be zero or greater")
	}
	if cfg.SMTPTimeout <= 0 {
		return fmt.Errorf("SMTP_TIMEOUT must be greater than zero")
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify pass**

Run:

```bash
go test ./internal/config
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: validate production config"
```

---

### Task 2: HTTP Server Timeout Wiring

**Files:**
- Modify: `cmd/newsletter/main.go`
- Create: `cmd/newsletter/main_test.go`

**Interfaces:**
- Consumes: `config.Config.HTTPAddr`
- Produces: `newHTTPServer(cfg config.Config, handler http.Handler) *http.Server`
- Later tasks do not need to know about the helper; it exists to make timeout wiring testable.

- [ ] **Step 1: Write the failing timeout test**

Create `cmd/newsletter/main_test.go`:

```go
package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
)

func TestNewHTTPServerUsesProductionTimeouts(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer(config.Config{HTTPAddr: ":8080"}, handler)

	if server.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", server.Addr)
	}
	if server.Handler != handler {
		t.Fatal("Handler was not preserved")
	}
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Second {
		t.Fatalf("ReadTimeout = %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Fatalf("WriteTimeout = %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./cmd/newsletter
```

Expected: FAIL because `newHTTPServer` is not defined.

- [ ] **Step 3: Implement the helper and use it**

In `cmd/newsletter/main.go`, replace the existing `server := &http.Server{...}` literal with:

```go
server := newHTTPServer(cfg, loggingMiddleware(logger, handler))
```

Add this helper near `loggingMiddleware`:

```go
func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
```

- [ ] **Step 4: Run tests to verify pass**

Run:

```bash
go test ./cmd/newsletter
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/newsletter/main.go cmd/newsletter/main_test.go
git commit -m "feat: configure http server timeouts"
```

---

### Task 3: Proxy-Aware Client Identity And Bounded Rate Limiting

**Files:**
- Modify: `internal/httpserver/server.go`
- Modify: `internal/httpserver/handlers.go`
- Modify: `internal/httpserver/handlers_test.go`
- Modify: `internal/ratelimit/ratelimit.go`
- Modify: `internal/ratelimit/ratelimit_test.go`

**Interfaces:**
- Consumes: `config.Config.TrustProxyHeaders`
- Produces: `clientIP(r *http.Request, trustProxyHeaders bool) string`
- Produces: `ratelimit.(*Limiter).Allow(key string) bool` with stale bucket pruning

- [ ] **Step 1: Write failing client IP tests**

Add these tests to `internal/httpserver/handlers_test.go`:

```go
func TestClientIPIgnoresForwardedForWhenProxyTrustDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/subscribe", nil)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")

	got := clientIP(req, false)
	if got != "198.51.100.20" {
		t.Fatalf("clientIP() = %q, want remote address", got)
	}
}

func TestClientIPUsesForwardedForWhenProxyTrustEnabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/subscribe", nil)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 203.0.113.11")

	got := clientIP(req, true)
	if got != "203.0.113.10" {
		t.Fatalf("clientIP() = %q, want first forwarded IP", got)
	}
}
```

- [ ] **Step 2: Write failing limiter pruning test**

Add this test to `internal/ratelimit/ratelimit_test.go`:

```go
func TestLimiterPrunesOldMinuteBuckets(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	limiter := New(10, func() time.Time { return now })

	for _, key := range []string{"203.0.113.10", "203.0.113.11", "203.0.113.12"} {
		if !limiter.Allow(key) {
			t.Fatalf("first request for %s should be allowed", key)
		}
	}
	if len(limiter.hits) != 3 {
		t.Fatalf("hits before prune = %d, want 3", len(limiter.hits))
	}

	now = now.Add(2 * time.Minute)
	if !limiter.Allow("203.0.113.99") {
		t.Fatal("new-minute request should be allowed")
	}
	if len(limiter.hits) != 1 {
		t.Fatalf("hits after prune = %d, want 1", len(limiter.hits))
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/httpserver ./internal/ratelimit
```

Expected: FAIL because `clientIP` has the old signature and limiter pruning is not implemented.

- [ ] **Step 4: Implement proxy trust wiring**

In `internal/httpserver/server.go`, add a field:

```go
trustProxyHeaders bool
```

Set it in `New`:

```go
trustProxyHeaders: deps.Config.TrustProxyHeaders,
```

In `internal/httpserver/handlers.go`, update `allow`:

```go
func (s *server) allow(r *http.Request) bool {
	return s.limiter.Allow(clientIP(r, s.trustProxyHeaders))
}
```

Replace `clientIP` with:

```go
func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		forwarded := r.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if trimmed := strings.TrimSpace(first); trimmed != "" {
				return trimmed
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
```

- [ ] **Step 5: Implement limiter pruning**

In `internal/ratelimit/ratelimit.go`, update `Allow`:

```go
func (l *Limiter) Allow(key string) bool {
	if l.limit <= 0 {
		return true
	}
	minute := l.now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(minute)
	current := l.hits[key]
	if current.minute != minute {
		current = bucket{minute: minute}
	}
	if current.count >= l.limit {
		l.hits[key] = current
		return false
	}
	current.count++
	l.hits[key] = current
	return true
}

func (l *Limiter) pruneLocked(currentMinute int64) {
	for key, hit := range l.hits {
		if hit.minute < currentMinute {
			delete(l.hits, key)
		}
	}
}
```

- [ ] **Step 6: Run tests to verify pass**

Run:

```bash
go test ./internal/httpserver ./internal/ratelimit
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/httpserver/server.go internal/httpserver/handlers.go internal/httpserver/handlers_test.go internal/ratelimit/ratelimit.go internal/ratelimit/ratelimit_test.go
git commit -m "feat: harden rate limit client identity"
```

---

### Task 4: SMTP Network Deadlines

**Files:**
- Modify: `internal/mailer/smtp.go`
- Modify: `internal/mailer/smtp_test.go`

**Interfaces:**
- Consumes: `config.Config.SMTPTimeout`
- Produces: `SMTPSender.timeout time.Duration`
- Produces: `smtpClient` interface used by `sendWithClient`
- Produces: `newSMTPClient(conn net.Conn, host string) (smtpClient, error)`

- [ ] **Step 1: Write failing timeout storage test**

Add this test to `internal/mailer/smtp_test.go`:

```go
func TestNewSMTPSenderStoresTimeout(t *testing.T) {
	cfg := smtpTestConfig("starttls")
	cfg.SMTPTimeout = 30 * time.Second

	sender, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	if sender.timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", sender.timeout)
	}
}
```

Update the test file imports to include:

```go
import (
	"net"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
)
```

Update `smtpTestConfig(mode string)` to include:

```go
SMTPTimeout: 30 * time.Second,
```

- [ ] **Step 2: Write failing deadline application test**

Add a fake connection to `internal/mailer/smtp_test.go`:

```go
type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.deadline = t
	return nil
}
```

Add this test:

```go
func TestSMTPSenderAppliesConnectionDeadline(t *testing.T) {
	cfg := smtpTestConfig("starttls")
	cfg.SMTPTimeout = 30 * time.Second
	sender, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	conn := &deadlineConn{}
	before := time.Now()

	if err := sender.applyDeadline(conn); err != nil {
		t.Fatalf("applyDeadline() error = %v", err)
	}
	if conn.deadline.Before(before.Add(29*time.Second)) || conn.deadline.After(before.Add(31*time.Second)) {
		t.Fatalf("deadline = %s, want roughly 30s from now", conn.deadline)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./internal/mailer
```

Expected: FAIL because `SMTPSender.timeout` and `applyDeadline` do not exist.

- [ ] **Step 4: Implement SMTP client seams and deadlines**

In `internal/mailer/smtp.go`, add `io` to imports.

Add interfaces near `SMTPSender`:

```go
type smtpClient interface {
	StartTLS(config *tls.Config) error
	Auth(auth smtp.Auth) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

type smtpClientFactory func(conn net.Conn, host string) (smtpClient, error)
```

Add fields to `SMTPSender`:

```go
timeout       time.Duration
clientFactory smtpClientFactory
```

In `NewSMTPSender`, set:

```go
timeout:       cfg.SMTPTimeout,
clientFactory: newSMTPClient,
```

Add helpers:

```go
func newSMTPClient(conn net.Conn, host string) (smtpClient, error) {
	return smtp.NewClient(conn, host)
}

func (s *SMTPSender) applyDeadline(conn net.Conn) error {
	return conn.SetDeadline(time.Now().Add(s.timeout))
}

func (s *SMTPSender) dialPlain(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := s.applyDeadline(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (s *SMTPSender) dialTLS(ctx context.Context, addr string) (net.Conn, error) {
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: s.timeout},
		Config:    &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12},
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := s.applyDeadline(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
```

Update `Send` to pass context:

```go
case "none":
	return s.sendPlain(ctx, addr, auth, msg, payload)
case "starttls":
	return s.sendStartTLS(ctx, addr, auth, msg, payload)
case "tls":
	return s.sendTLS(ctx, addr, auth, msg, payload)
```

Implement the send methods:

```go
func (s *SMTPSender) sendPlain(ctx context.Context, addr string, auth smtp.Auth, msg Message, payload []byte) error {
	conn, err := s.dialPlain(ctx, addr)
	if err != nil {
		return err
	}
	client, err := s.clientFactory(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	return sendWithClient(client, auth, msg, payload)
}

func (s *SMTPSender) sendStartTLS(ctx context.Context, addr string, auth smtp.Auth, msg Message, payload []byte) error {
	conn, err := s.dialPlain(ctx, addr)
	if err != nil {
		return err
	}
	client, err := s.clientFactory(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	return sendWithClient(client, auth, msg, payload)
}

func (s *SMTPSender) sendTLS(ctx context.Context, addr string, auth smtp.Auth, msg Message, payload []byte) error {
	conn, err := s.dialTLS(ctx, addr)
	if err != nil {
		return err
	}
	client, err := s.clientFactory(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	return sendWithClient(client, auth, msg, payload)
}
```

Change `sendWithClient` signature:

```go
func sendWithClient(client smtpClient, auth smtp.Auth, msg Message, payload []byte) error {
```

- [ ] **Step 5: Run tests to verify pass**

Run:

```bash
go test ./internal/mailer
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/mailer/smtp.go internal/mailer/smtp_test.go
git commit -m "feat: add smtp network deadlines"
```

---

### Task 5: Atomic Token Consumption

**Files:**
- Modify: `internal/store/sqlstore.go`
- Modify: `internal/store/sqlstore_test.go`

**Interfaces:**
- Consumes: `store.ErrNotFound`
- Produces: `useTokenTx(ctx context.Context, tx *sqlx.Tx, hash string, purpose TokenPurpose, now time.Time) (Token, error)` with update-first semantics

- [ ] **Step 1: Write expired token regression test**

Add this test to `internal/store/sqlstore_test.go`:

```go
func TestUseTokenRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "expired-token", now.Add(-time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	if _, err := st.UseToken(ctx, "expired-token", TokenConfirmSubscribe, now); err == nil {
		t.Fatal("UseToken() error = nil, want expired token error")
	}
}
```

- [ ] **Step 2: Write direct double-use regression test**

Add this test to `internal/store/sqlstore_test.go`:

```go
func TestUseTokenRejectsSecondUse(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "one-time-token", now.Add(time.Hour), now); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	first, err := st.UseToken(ctx, "one-time-token", TokenConfirmSubscribe, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("first UseToken() error = %v", err)
	}
	if first.UsedAt == nil {
		t.Fatal("first token UsedAt = nil")
	}
	if _, err := st.UseToken(ctx, "one-time-token", TokenConfirmSubscribe, now.Add(2*time.Minute)); err == nil {
		t.Fatal("second UseToken() error = nil, want already-used error")
	}
}
```

- [ ] **Step 3: Run tests to establish current behavior**

Run:

```bash
go test ./internal/store
```

Expected: PASS today because existing select-before-update already rejects normal sequential reuse. These tests preserve the expected behavior while Task 5 changes the implementation to atomic update-first semantics.

- [ ] **Step 4: Implement update-first token consumption**

Replace `useTokenTx` in `internal/store/sqlstore.go` with:

```go
func (s *SQLStore) useTokenTx(ctx context.Context, tx *sqlx.Tx, hash string, purpose TokenPurpose, now time.Time) (Token, error) {
	res, err := tx.ExecContext(ctx, s.rebind(`
UPDATE tokens
SET used_at = ?
WHERE hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?`), formatTime(now), hash, string(purpose), formatTime(now))
	if err != nil {
		return Token{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Token{}, err
	}
	if affected == 0 {
		return Token{}, ErrNotFound
	}
	tok, err := scanToken(tx.QueryRowxContext(ctx, s.rebind(`
SELECT id, subscriber_id, purpose, hash, expires_at, created_at, used_at
FROM tokens
WHERE hash = ? AND purpose = ?`), hash, string(purpose)))
	if err != nil {
		return Token{}, err
	}
	return tok, nil
}
```

This makes the conditional update the first database operation. Concurrent consumers race on the `UPDATE`; only one can affect a row because the second sees `used_at IS NOT NULL`.

- [ ] **Step 5: Run tests to verify pass**

Run:

```bash
go test ./internal/store
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlstore.go internal/store/sqlstore_test.go
git commit -m "feat: consume tokens atomically"
```

---

### Task 6: Deployment And Documentation Polish

**Files:**
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `README.md`

**Interfaces:**
- Consumes: `TRUST_PROXY_HEADERS`
- Consumes: `SMTP_TIMEOUT`
- Produces: documented single-instance deployment assumptions and safer Compose defaults

- [ ] **Step 1: Update `.env.example`**

Edit `.env.example` so the database credentials are visibly placeholders and the new settings exist:

```dotenv
HTTP_ADDR=:8080
PUBLIC_BASE_URL=https://newsletter.example.com
NEWSLETTER_NAME=Example Newsletter

POSTGRES_USER=newsletter
POSTGRES_PASSWORD=change-me-postgres-password
POSTGRES_DB=newsletter
DATABASE_URL=postgres://newsletter:change-me-postgres-password@postgres:5432/newsletter?sslmode=disable

SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=smtp-user
SMTP_PASSWORD=change-me-smtp-password
SMTP_FROM_EMAIL=news@example.com
SMTP_FROM_NAME=Example Newsletter
SMTP_TLS_MODE=starttls
SMTP_TIMEOUT=30s

PUBLIC_DIR=/app/public
ASSETS_DIR=/app/public/assets
SUBSCRIBE_SUCCESS_PAGE=subscribe-success.html
UNSUBSCRIBE_SUCCESS_PAGE=unsubscribe-success.html
TOKEN_ERROR_PAGE=token-error.html

NEWSLETTER_DIR=/app/newsletters
NEWSLETTER_SCAN_INTERVAL=30s
SEND_DELAY=2s
MAX_SEND_ATTEMPTS=3
TOKEN_TTL=24h
RATE_LIMIT_PER_MINUTE=5
TRUST_PROXY_HEADERS=true

TRAEFIK_HOST=newsletter.example.com
TRAEFIK_ENTRYPOINT=websecure
TRAEFIK_CERT_RESOLVER=letsencrypt
```

- [ ] **Step 2: Update Compose Postgres environment interpolation**

In `docker-compose.yml`, replace the hard-coded Postgres values with:

```yaml
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
```

Update the healthcheck command to use the interpolated values:

```yaml
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5
```

Keep no `ports:` entry on the app service.

- [ ] **Step 3: Update README configuration list**

In `README.md`, update required groups:

```markdown
- SMTP: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM_EMAIL`, `SMTP_FROM_NAME`, `SMTP_TLS_MODE`, `SMTP_TIMEOUT`
- Sending and abuse protection: `SEND_DELAY`, `MAX_SEND_ATTEMPTS`, `TOKEN_TTL`, `RATE_LIMIT_PER_MINUTE`, `TRUST_PROXY_HEADERS`
```

Update the local run command by adding:

```text
SMTP_TIMEOUT=30s TRUST_PROXY_HEADERS=false
```

Use the local command as one line, matching the existing README style.

- [ ] **Step 4: Add README production notes**

Add this section before `## Public Endpoints`:

```markdown
## Production Notes

- Replace all placeholder secrets in `.env` before deployment.
- Keep `DATABASE_URL` credentials aligned with `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB`.
- This version supports a single app instance against the database. Do not run multiple app replicas for newsletter sending.
- Set `TRUST_PROXY_HEADERS=true` only when the app is reachable exclusively through Traefik or another trusted proxy that controls inbound forwarding headers.
- Do not publish the app container port directly to the public internet.
- Prefer `SMTP_TLS_MODE=starttls` or `tls` in production. Use `none` only for local SMTP capture tools.
- The app image is distroless and does not include curl, wget, or a shell. Use Traefik or an external monitor to check `GET /healthz`.
```

- [ ] **Step 5: Validate docs and Compose syntax**

Run:

```bash
docker compose --env-file .env.example config
```

Expected: PASS and rendered services show no app `ports:` mapping. If Docker Compose is unavailable locally, record the command failure in the task handoff and still run the final Go checks in Task 7.

- [ ] **Step 6: Commit**

```bash
git add .env.example docker-compose.yml README.md
git commit -m "docs: document production deployment settings"
```

---

### Task 7: Final Verification

**Files:**
- No source edits expected.
- Review: all files changed by Tasks 1-6.

**Interfaces:**
- Consumes: all task deliverables.
- Produces: verified production-hardening branch state.

- [ ] **Step 1: Inspect status**

Run:

```bash
git status --short
```

Expected: clean worktree before verification. If dirty files exist from a previous task, inspect them and either commit the intended changes or stop for review.

- [ ] **Step 2: Run full tests**

Run:

```bash
go test ./...
```

Expected: PASS for all packages.

- [ ] **Step 3: Run static checks**

Run:

```bash
go vet ./...
```

Expected: no output and exit code 0.

- [ ] **Step 4: Confirm spec acceptance criteria**

Check these concrete conditions:

```bash
rg -n "TrustProxyHeaders|SMTPTimeout|TRUST_PROXY_HEADERS|SMTP_TIMEOUT" internal .env.example README.md
rg -n "ReadTimeout|WriteTimeout|IdleTimeout|ReadHeaderTimeout" cmd/newsletter/main.go
rg -n "SetDeadline|DialContext|tls.Dialer" internal/mailer/smtp.go
rg -n "used_at IS NULL|RowsAffected" internal/store/sqlstore.go
rg -n "POSTGRES_PASSWORD: newsletter|newsletter:newsletter" docker-compose.yml .env.example README.md
```

Expected:

- First four commands find the new hardening code and docs.
- Last command returns no matches.

- [ ] **Step 5: Commit verification notes only if files changed**

If Task 7 required doc corrections, commit them:

```bash
git add README.md .env.example docker-compose.yml
git commit -m "docs: clarify production hardening verification"
```

If no files changed, do not create an empty commit.
