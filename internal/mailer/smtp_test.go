package mailer

import (
	"net"
	"testing"
	"time"

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

type deadlineConn struct {
	net.Conn
	deadline time.Time
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.deadline = t
	return nil
}

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

func TestSMTPSenderAppliesConfiguredFromDefaults(t *testing.T) {
	sender, err := NewSMTPSender(smtpTestConfig("none"))
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	msg := sender.messageWithDefaults(Message{ToEmail: "user@example.com", Subject: "Subject", HTML: []byte("<p>Hello</p>")})

	if msg.FromEmail != "news@example.com" || msg.FromName != "Example" {
		t.Fatalf("from defaults = %+v", msg)
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
		SMTPTimeout:   30 * time.Second,
	}
}
