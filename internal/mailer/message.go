package mailer

import (
	"context"
	"fmt"
	"html/template"
	"strings"
)

type Sender interface {
	Send(ctx context.Context, msg Message) error
}

type Message struct {
	FromEmail string
	FromName  string
	ToEmail   string
	Subject   string
	HTML      []byte
}

func NewConfirmationMessage(fromEmail string, fromName string, toEmail string, baseURL string, plainToken string) Message {
	link := strings.TrimRight(baseURL, "/") + "/confirm?token=" + plainToken
	body := fmt.Sprintf(`<p>Please confirm your subscription by opening <a href="%s">this link</a>.</p>`, escape(link))
	return Message{
		FromEmail: fromEmail,
		FromName:  fromName,
		ToEmail:   toEmail,
		Subject:   "Confirm your subscription",
		HTML:      []byte(body),
	}
}

func NewUnsubscribeMessage(fromEmail string, fromName string, toEmail string, baseURL string, plainToken string) Message {
	link := strings.TrimRight(baseURL, "/") + "/unsubscribe/confirm?token=" + plainToken
	body := fmt.Sprintf(`<p>Please confirm your unsubscribe request by opening <a href="%s">this link</a>.</p>`, escape(link))
	return Message{
		FromEmail: fromEmail,
		FromName:  fromName,
		ToEmail:   toEmail,
		Subject:   "Confirm unsubscribe request",
		HTML:      []byte(body),
	}
}

func NewNewsletterMessage(fromEmail string, fromName string, toEmail string, subject string, html []byte) Message {
	return Message{
		FromEmail: fromEmail,
		FromName:  fromName,
		ToEmail:   toEmail,
		Subject:   subject,
		HTML:      html,
	}
}

func escape(value string) string {
	return template.HTMLEscapeString(value)
}
