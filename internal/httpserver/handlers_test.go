package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/config"
	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/store"
)

func TestSubscribeRejectsInvalidEmail(t *testing.T) {
	server := newTestServer(&fakeStore{}, &fakeSender{})
	resp := postForm(server, "/subscribe", "email=not-an-email")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSubscribeCreatesPendingSubscriberTokenAndSendsConfirmation(t *testing.T) {
	st := &fakeStore{}
	sender := &fakeSender{}
	server := newTestServer(st, sender)
	resp := postForm(server, "/subscribe", "email=USER@example.COM")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if st.pendingEmail != "user@example.com" {
		t.Fatalf("pendingEmail = %q", st.pendingEmail)
	}
	if st.createdToken.Purpose != store.TokenConfirmSubscribe || st.createdToken.Hash == "" {
		t.Fatalf("createdToken = %+v", st.createdToken)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %d", len(sender.messages))
	}
	if !strings.Contains(string(sender.messages[0].HTML), "/confirm?token=") {
		t.Fatalf("confirmation message = %s", sender.messages[0].HTML)
	}
}

func TestConfirmActivatesSubscriberByHashedToken(t *testing.T) {
	st := &fakeStore{}
	server := newTestServer(st, &fakeSender{})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/confirm?token=plain-token", nil)
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if st.activatedHash == "" || st.activatedHash == "plain-token" {
		t.Fatalf("activatedHash = %q", st.activatedHash)
	}
}

func TestUnsubscribeDoesNotRevealUnknownEmail(t *testing.T) {
	st := &fakeStore{findSubscriber: false}
	sender := &fakeSender{}
	server := newTestServer(st, sender)
	resp := postForm(server, "/unsubscribe", "email=missing@example.com")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sent messages = %d", len(sender.messages))
	}
}

func TestUnsubscribeSendsTokenWhenSubscriberExists(t *testing.T) {
	st := &fakeStore{findSubscriber: true}
	sender := &fakeSender{}
	server := newTestServer(st, sender)
	resp := postForm(server, "/unsubscribe", "email=user@example.com")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if st.createdToken.Purpose != store.TokenConfirmUnsubscribe {
		t.Fatalf("createdToken = %+v", st.createdToken)
	}
	if len(sender.messages) != 1 || !strings.Contains(string(sender.messages[0].HTML), "/unsubscribe/confirm?token=") {
		t.Fatalf("messages = %+v", sender.messages)
	}
}

func TestUnsubscribeConfirmUsesHashedToken(t *testing.T) {
	st := &fakeStore{}
	server := newTestServer(st, &fakeSender{})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/unsubscribe/confirm?token=plain-token", nil)
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if st.unsubscribedHash == "" || st.unsubscribedHash == "plain-token" {
		t.Fatalf("unsubscribedHash = %q", st.unsubscribedHash)
	}
}

func TestHealthzReturnsOKWhenStorePingSucceeds(t *testing.T) {
	server := newTestServer(&fakeStore{}, &fakeSender{})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	server.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
}

func TestClientIPIgnoresForwardedForWhenProxyTrustDisabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/subscribe", nil)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")

	got := clientIP(req, false)
	if got != "198.51.100.20" {
		t.Fatalf("clientIP() = %q, want remote address", got)
	}
}

func TestClientIPUsesForwardedForWhenProxyTrustEnabled(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/subscribe", nil)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 203.0.113.11")

	got := clientIP(req, true)
	if got != "203.0.113.10" {
		t.Fatalf("clientIP() = %q, want first forwarded IP", got)
	}
}

func newTestServer(st store.Store, sender mailer.Sender) http.Handler {
	return New(Dependencies{
		Store:  st,
		Sender: sender,
		Config: config.Config{
			PublicBaseURL:      "https://newsletter.example.com",
			SMTPFromEmail:      "news@example.com",
			SMTPFromName:       "Example",
			TokenTTL:           time.Hour,
			RateLimitPerMinute: 100,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock: func() time.Time {
			return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		},
	})
}

func postForm(handler http.Handler, target string, body string) *http.Response {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp.Result()
}

type fakeSender struct {
	messages []mailer.Message
}

func (f *fakeSender) Send(ctx context.Context, msg mailer.Message) error {
	f.messages = append(f.messages, msg)
	return nil
}

type fakeStore struct {
	pendingEmail     string
	createdToken     store.Token
	activatedHash    string
	unsubscribedHash string
	findSubscriber   bool
}

func (f *fakeStore) UpsertPendingSubscriber(ctx context.Context, email string, now time.Time) (store.Subscriber, error) {
	f.pendingEmail = email
	return store.Subscriber{ID: "sub-1", Email: email, Status: store.SubscriberPending}, nil
}

func (f *fakeStore) FindSubscriberByEmail(ctx context.Context, email string) (store.Subscriber, bool, error) {
	if !f.findSubscriber {
		return store.Subscriber{}, false, nil
	}
	return store.Subscriber{ID: "sub-1", Email: email, Status: store.SubscriberActive}, true, nil
}

func (f *fakeStore) CreateToken(ctx context.Context, subscriberID string, purpose store.TokenPurpose, hash string, expiresAt time.Time, now time.Time) (store.Token, error) {
	f.createdToken = store.Token{ID: "tok-1", SubscriberID: subscriberID, Purpose: purpose, Hash: hash, ExpiresAt: expiresAt, CreatedAt: now}
	return f.createdToken, nil
}

func (f *fakeStore) UseToken(ctx context.Context, hash string, purpose store.TokenPurpose, now time.Time) (store.Token, error) {
	return store.Token{}, errors.New("not used by http tests")
}

func (f *fakeStore) ActivateSubscriberByToken(ctx context.Context, hash string, now time.Time) (store.Subscriber, error) {
	f.activatedHash = hash
	return store.Subscriber{ID: "sub-1", Email: "user@example.com", Status: store.SubscriberActive}, nil
}

func (f *fakeStore) UnsubscribeSubscriberByToken(ctx context.Context, hash string, now time.Time) (store.Subscriber, error) {
	f.unsubscribedHash = hash
	return store.Subscriber{ID: "sub-1", Email: "user@example.com", Status: store.SubscriberUnsubscribed}, nil
}

func (f *fakeStore) CreateCampaignIfNew(ctx context.Context, input store.CampaignInput, now time.Time) (store.Campaign, bool, error) {
	return store.Campaign{}, false, errors.New("not used by http tests")
}

func (f *fakeStore) CreateDeliveriesForCampaign(ctx context.Context, campaignID string, now time.Time) (int, error) {
	return 0, errors.New("not used by http tests")
}

func (f *fakeStore) ClaimPendingDelivery(ctx context.Context, maxAttempts int, now time.Time) (store.DeliveryJob, bool, error) {
	return store.DeliveryJob{}, false, errors.New("not used by http tests")
}

func (f *fakeStore) MarkDeliverySent(ctx context.Context, deliveryID string, now time.Time) error {
	return errors.New("not used by http tests")
}

func (f *fakeStore) MarkDeliveryFailed(ctx context.Context, deliveryID string, lastError string, maxAttempts int, now time.Time) error {
	return errors.New("not used by http tests")
}

func (f *fakeStore) Ping(ctx context.Context) error {
	return nil
}
