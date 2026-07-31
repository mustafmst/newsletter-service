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
