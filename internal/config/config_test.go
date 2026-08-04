package config

import (
	"strings"
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
	setValidEnv(t)
	t.Setenv("TRUST_PROXY_HEADERS", "true")

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
	if !cfg.TrustProxyHeaders {
		t.Fatal("TrustProxyHeaders = false, want true")
	}
	if cfg.SMTPTimeout != 20*time.Second {
		t.Fatalf("SMTPTimeout = %s", cfg.SMTPTimeout)
	}
}

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
	t.Setenv("SMTP_TIMEOUT", "20s")
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
		{name: "smtp timeout equals HTTP write timeout", key: "SMTP_TIMEOUT", raw: "30s"},
		{name: "smtp timeout exceeds HTTP write timeout", key: "SMTP_TIMEOUT", raw: "31s"},
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

func TestLoadRejectsMalformedTrustProxyHeaders(t *testing.T) {
	setValidEnv(t)
	t.Setenv("TRUST_PROXY_HEADERS", "not-a-bool")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want malformed boolean error")
	}
	if !strings.Contains(err.Error(), "TRUST_PROXY_HEADERS") {
		t.Fatalf("Load() error = %q, want TRUST_PROXY_HEADERS", err)
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
