package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const HTTPWriteTimeout = 30 * time.Second

type Config struct {
	HTTPAddr               string
	PublicBaseURL          string
	NewsletterName         string
	DatabaseURL            string
	SMTPHost               string
	SMTPPort               int
	SMTPUsername           string
	SMTPPassword           string
	SMTPFromEmail          string
	SMTPFromName           string
	SMTPTLSMode            string
	PublicDir              string
	AssetsDir              string
	SubscribeSuccessPage   string
	UnsubscribeSuccessPage string
	TokenErrorPage         string
	NewsletterDir          string
	NewsletterScanInterval time.Duration
	SendDelay              time.Duration
	MaxSendAttempts        int
	TokenTTL               time.Duration
	RateLimitPerMinute     int
	TrustProxyHeaders      bool
	SMTPTimeout            time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error

	if cfg.HTTPAddr, err = required("HTTP_ADDR"); err != nil {
		return Config{}, err
	}
	if cfg.PublicBaseURL, err = required("PUBLIC_BASE_URL"); err != nil {
		return Config{}, err
	}
	if cfg.NewsletterName, err = required("NEWSLETTER_NAME"); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseURL, err = required("DATABASE_URL"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPHost, err = required("SMTP_HOST"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPPort, err = parseInt("SMTP_PORT"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPUsername, err = required("SMTP_USERNAME"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPPassword, err = required("SMTP_PASSWORD"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPFromEmail, err = required("SMTP_FROM_EMAIL"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPFromName, err = required("SMTP_FROM_NAME"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPTLSMode, err = required("SMTP_TLS_MODE"); err != nil {
		return Config{}, err
	}
	switch cfg.SMTPTLSMode {
	case "starttls", "tls", "none":
	default:
		return Config{}, fmt.Errorf("SMTP_TLS_MODE must be starttls, tls, or none")
	}
	if cfg.PublicDir, err = required("PUBLIC_DIR"); err != nil {
		return Config{}, err
	}
	if cfg.AssetsDir, err = required("ASSETS_DIR"); err != nil {
		return Config{}, err
	}
	if cfg.SubscribeSuccessPage, err = required("SUBSCRIBE_SUCCESS_PAGE"); err != nil {
		return Config{}, err
	}
	if cfg.UnsubscribeSuccessPage, err = required("UNSUBSCRIBE_SUCCESS_PAGE"); err != nil {
		return Config{}, err
	}
	if cfg.TokenErrorPage, err = required("TOKEN_ERROR_PAGE"); err != nil {
		return Config{}, err
	}
	if cfg.NewsletterDir, err = required("NEWSLETTER_DIR"); err != nil {
		return Config{}, err
	}
	if cfg.NewsletterScanInterval, err = parseDuration("NEWSLETTER_SCAN_INTERVAL"); err != nil {
		return Config{}, err
	}
	if cfg.SendDelay, err = parseDuration("SEND_DELAY"); err != nil {
		return Config{}, err
	}
	if cfg.MaxSendAttempts, err = parseInt("MAX_SEND_ATTEMPTS"); err != nil {
		return Config{}, err
	}
	if cfg.TokenTTL, err = parseDuration("TOKEN_TTL"); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitPerMinute, err = parseInt("RATE_LIMIT_PER_MINUTE"); err != nil {
		return Config{}, err
	}
	if cfg.TrustProxyHeaders, err = parseBool("TRUST_PROXY_HEADERS"); err != nil {
		return Config{}, err
	}
	if cfg.SMTPTimeout, err = parseDuration("SMTP_TIMEOUT"); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func required(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func parseInt(key string) (int, error) {
	raw, err := required(key)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return value, nil
}

func parseDuration(key string) (time.Duration, error) {
	raw, err := required(key)
	if err != nil {
		return 0, err
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return value, nil
}

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
	if cfg.SMTPTimeout >= HTTPWriteTimeout {
		return fmt.Errorf("SMTP_TIMEOUT must be less than the HTTP write timeout of %s", HTTPWriteTimeout)
	}
	return nil
}
