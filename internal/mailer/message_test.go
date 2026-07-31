package mailer

import (
	"strings"
	"testing"
)

func TestNewConfirmationMessageIncludesTokenLink(t *testing.T) {
	msg := NewConfirmationMessage("news@example.com", "Example", "user@example.com", "https://newsletter.example.com", "plain-token")

	if msg.ToEmail != "user@example.com" {
		t.Fatalf("ToEmail = %q", msg.ToEmail)
	}
	if msg.Subject != "Confirm your subscription" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(string(msg.HTML), "https://newsletter.example.com/confirm?token=plain-token") {
		t.Fatalf("confirmation body missing token link: %s", msg.HTML)
	}
}

func TestNewUnsubscribeMessageIncludesTokenLink(t *testing.T) {
	msg := NewUnsubscribeMessage("news@example.com", "Example", "user@example.com", "https://newsletter.example.com", "plain-token")

	if msg.Subject != "Confirm unsubscribe request" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if !strings.Contains(string(msg.HTML), "https://newsletter.example.com/unsubscribe/confirm?token=plain-token") {
		t.Fatalf("unsubscribe body missing token link: %s", msg.HTML)
	}
}

func TestNewNewsletterMessagePreservesSubjectAndHTML(t *testing.T) {
	msg := NewNewsletterMessage("news@example.com", "Example", "user@example.com", "Weekly", []byte("<h1>Hello</h1>"))

	if msg.FromEmail != "news@example.com" || msg.FromName != "Example" || msg.ToEmail != "user@example.com" {
		t.Fatalf("addresses = %+v", msg)
	}
	if msg.Subject != "Weekly" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
	if string(msg.HTML) != "<h1>Hello</h1>" {
		t.Fatalf("HTML = %q", msg.HTML)
	}
}
