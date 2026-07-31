package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/pmstowski/newsletter/internal/config"
)

type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	tlsMode  string
}

func NewSMTPSender(cfg config.Config) (*SMTPSender, error) {
	switch cfg.SMTPTLSMode {
	case "starttls", "tls", "none":
	default:
		return nil, fmt.Errorf("unsupported SMTP TLS mode %q", cfg.SMTPTLSMode)
	}
	return &SMTPSender{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		tlsMode:  cfg.SMTPTLSMode,
	}, nil
}

func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	auth := smtp.PlainAuth("", s.username, s.password, s.host)
	payload := buildMIMEMessage(msg)

	switch s.tlsMode {
	case "none":
		return smtp.SendMail(addr, auth, msg.FromEmail, []string{msg.ToEmail}, payload)
	case "starttls":
		return s.sendStartTLS(addr, auth, msg, payload)
	case "tls":
		return s.sendTLS(addr, auth, msg, payload)
	default:
		return errors.New("unsupported SMTP TLS mode")
	}
}

func (s *SMTPSender) sendStartTLS(addr string, auth smtp.Auth, msg Message, payload []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	return sendWithClient(client, auth, msg, payload)
}

func (s *SMTPSender) sendTLS(addr string, auth smtp.Auth, msg Message, payload []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	return sendWithClient(client, auth, msg, payload)
}

func sendWithClient(client *smtp.Client, auth smtp.Auth, msg Message, payload []byte) error {
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
