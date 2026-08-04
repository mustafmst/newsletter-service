package mailer

import (
	"context"
	"errors"
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

var errTLSHandshakeRead = errors.New("TLS handshake read")

type handshakeDeadlineConn struct {
	deadline time.Time
}

func (c *handshakeDeadlineConn) Read([]byte) (int, error) {
	if c.deadline.IsZero() {
		return 0, errors.New("TLS handshake read before deadline")
	}
	return 0, errTLSHandshakeRead
}

func (*handshakeDeadlineConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (*handshakeDeadlineConn) Close() error                     { return nil }
func (*handshakeDeadlineConn) LocalAddr() net.Addr              { return nil }
func (*handshakeDeadlineConn) RemoteAddr() net.Addr             { return nil }
func (c *handshakeDeadlineConn) SetDeadline(t time.Time) error  { c.deadline = t; return nil }
func (*handshakeDeadlineConn) SetReadDeadline(time.Time) error  { return nil }
func (*handshakeDeadlineConn) SetWriteDeadline(time.Time) error { return nil }

func TestSMTPSenderSendTLSAppliesDeadlineBeforeHandshake(t *testing.T) {
	cfg := smtpTestConfig("tls")
	cfg.SMTPTimeout = 30 * time.Second
	sender, err := NewSMTPSender(cfg)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	conn := &handshakeDeadlineConn{}
	sender.dialContext = func(context.Context, string, string) (net.Conn, error) {
		return conn, nil
	}

	err = sender.Send(context.Background(), Message{
		ToEmail: "user@example.com",
		Subject: "Subject",
		HTML:    []byte("<p>Hello</p>"),
	})
	if !errors.Is(err, errTLSHandshakeRead) {
		t.Fatalf("Send() error = %v, want TLS handshake read after deadline", err)
	}
	if conn.deadline.IsZero() {
		t.Fatal("Send() did not set a deadline before the TLS handshake")
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
