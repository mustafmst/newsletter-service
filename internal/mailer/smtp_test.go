package mailer

import (
	"testing"

	"github.com/pmstowski/newsletter/internal/config"
)

func TestNewSMTPSenderAcceptsSupportedTLSModes(t *testing.T) {
	for _, mode := range []string{"starttls", "tls", "none"} {
		cfg := smtpTestConfig(mode)
		if _, err := NewSMTPSender(cfg); err != nil {
			t.Fatalf("NewSMTPSender(%q) error = %v", mode, err)
		}
	}
}

func TestNewSMTPSenderRejectsUnsupportedTLSMode(t *testing.T) {
	cfg := smtpTestConfig("weird")
	if _, err := NewSMTPSender(cfg); err == nil {
		t.Fatal("expected unsupported TLS mode error")
	}
}

func smtpTestConfig(mode string) config.Config {
	return config.Config{
		SMTPHost:      "smtp.example.com",
		SMTPPort:      587,
		SMTPUsername:  "user",
		SMTPPassword:  "pass",
		SMTPFromEmail: "news@example.com",
		SMTPFromName:  "Example",
		SMTPTLSMode:   mode,
	}
}
