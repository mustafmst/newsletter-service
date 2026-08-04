package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
)

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

type smtpDialContext func(ctx context.Context, network, address string) (net.Conn, error)

type SMTPSender struct {
	host          string
	port          int
	username      string
	password      string
	fromEmail     string
	fromName      string
	tlsMode       string
	timeout       time.Duration
	clientFactory smtpClientFactory
	dialContext   smtpDialContext
}

func NewSMTPSender(cfg config.Config) (*SMTPSender, error) {
	switch cfg.SMTPTLSMode {
	case "starttls", "tls", "none":
	default:
		return nil, fmt.Errorf("unsupported SMTP TLS mode %q", cfg.SMTPTLSMode)
	}
	return &SMTPSender{
		host:          cfg.SMTPHost,
		port:          cfg.SMTPPort,
		username:      cfg.SMTPUsername,
		password:      cfg.SMTPPassword,
		fromEmail:     cfg.SMTPFromEmail,
		fromName:      cfg.SMTPFromName,
		tlsMode:       cfg.SMTPTLSMode,
		timeout:       cfg.SMTPTimeout,
		clientFactory: newSMTPClient,
		dialContext:   (&net.Dialer{Timeout: cfg.SMTPTimeout}).DialContext,
	}, nil
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	msg = s.messageWithDefaults(msg)
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	payload := buildMIMEMessage(msg)

	switch s.tlsMode {
	case "none":
		return s.sendPlain(ctx, addr, auth, msg, payload)
	case "starttls":
		return s.sendStartTLS(ctx, addr, auth, msg, payload)
	case "tls":
		return s.sendTLS(ctx, addr, auth, msg, payload)
	default:
		return errors.New("unsupported SMTP TLS mode")
	}
}

func (s *SMTPSender) messageWithDefaults(msg Message) Message {
	if msg.FromEmail == "" {
		msg.FromEmail = s.fromEmail
	}
	if msg.FromName == "" {
		msg.FromName = s.fromName
	}
	return msg
}

func newSMTPClient(conn net.Conn, host string) (smtpClient, error) {
	return smtp.NewClient(conn, host)
}

func (s *SMTPSender) applyDeadline(conn net.Conn) error {
	return conn.SetDeadline(time.Now().Add(s.timeout))
}

func (s *SMTPSender) dialPlain(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := s.dialContext(ctx, "tcp", addr)
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
	conn, err := s.dialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := s.applyDeadline(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

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

func sendWithClient(client smtpClient, auth smtp.Auth, msg Message, payload []byte) error {
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(msg.FromEmail); err != nil {
		return err
	}
	if err := client.Rcpt(msg.ToEmail); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildMIMEMessage(msg Message) []byte {
	var buf bytes.Buffer
	from := mail.Address{Name: msg.FromName, Address: msg.FromEmail}
	to := mail.Address{Address: msg.ToEmail}
	headers := map[string]string{
		"From":         from.String(),
		"To":           to.String(),
		"Subject":      mime.QEncoding.Encode("utf-8", msg.Subject),
		"MIME-Version": "1.0",
		"Content-Type": `text/html; charset="UTF-8"`,
	}
	for _, key := range []string{"From", "To", "Subject", "MIME-Version", "Content-Type"} {
		buf.WriteString(key)
		buf.WriteString(": ")
		buf.WriteString(strings.ReplaceAll(headers[key], "\n", ""))
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	buf.Write(msg.HTML)
	return buf.Bytes()
}
