package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSubscriberLifecycle(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if sub.Email != "user@example.com" || sub.Status != SubscriberPending {
		t.Fatalf("subscriber = %+v", sub)
	}

	again, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second UpsertPendingSubscriber() error = %v", err)
	}
	if again.ID != sub.ID {
		t.Fatalf("duplicate subscriber id = %q, want %q", again.ID, sub.ID)
	}
}

func TestTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	_, err = st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "token-hash", now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	activated, err := st.ActivateSubscriberByToken(ctx, "token-hash", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ActivateSubscriberByToken() error = %v", err)
	}
	if activated.Status != SubscriberActive {
		t.Fatalf("status = %q", activated.Status)
	}

	if _, err := st.ActivateSubscriberByToken(ctx, "token-hash", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expected used token to fail")
	}
}

func TestUseTokenRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "expired-token", now.Add(-time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	if _, err := st.UseToken(ctx, "expired-token", TokenConfirmSubscribe, now); err == nil {
		t.Fatal("UseToken() error = nil, want expired token error")
	}
}

func TestUseTokenRejectsSecondUse(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "user@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "one-time-token", now.Add(time.Hour), now); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	first, err := st.UseToken(ctx, "one-time-token", TokenConfirmSubscribe, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("first UseToken() error = %v", err)
	}
	if first.UsedAt == nil {
		t.Fatal("first token UsedAt = nil")
	}
	if _, err := st.UseToken(ctx, "one-time-token", TokenConfirmSubscribe, now.Add(2*time.Minute)); err == nil {
		t.Fatal("second UseToken() error = nil, want already-used error")
	}
}

func TestUseTokenAllowsExactlyOneConcurrentConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "tokens.db") + "?_busy_timeout=5000"

	firstStore := openTestStoreAt(t, ctx, databaseURL)
	if err := firstStore.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	secondStore := openTestStoreAt(t, ctx, databaseURL)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	sub, err := firstStore.UpsertPendingSubscriber(ctx, "concurrent@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := firstStore.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "concurrent-token", now.Add(time.Hour), now); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan error, 2)
	consume := func(st *SQLStore) {
		ready <- struct{}{}
		<-start
		_, err := st.UseToken(ctx, "concurrent-token", TokenConfirmSubscribe, now.Add(time.Minute))
		results <- err
	}
	go consume(firstStore)
	go consume(secondStore)
	<-ready
	<-ready
	close(start)

	successes := 0
	notFound := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotFound):
			notFound++
		default:
			t.Fatalf("UseToken() concurrent error = %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("concurrent results: successes = %d, ErrNotFound = %d; want 1 and 1", successes, notFound)
	}
}

func TestCampaignHashIsUnique(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	first, created, err := st.CreateCampaignIfNew(ctx, CampaignInput{
		SourcePath:    "/news/a.html",
		ContentSHA256: "abc123",
		Subject:       "Subject",
		FromName:      "Example",
		HTML:          []byte("<p>Hello</p>"),
	}, now)
	if err != nil {
		t.Fatalf("CreateCampaignIfNew() error = %v", err)
	}
	if !created {
		t.Fatal("first campaign should be created")
	}

	second, created, err := st.CreateCampaignIfNew(ctx, CampaignInput{
		SourcePath:    "/news/copy.html",
		ContentSHA256: "abc123",
		Subject:       "Copy",
		FromName:      "Example",
		HTML:          []byte("<p>Hello</p>"),
	}, now)
	if err != nil {
		t.Fatalf("duplicate CreateCampaignIfNew() error = %v", err)
	}
	if created {
		t.Fatal("duplicate hash should not create a campaign")
	}
	if second.ID != first.ID || second.Subject != "Subject" {
		t.Fatalf("duplicate campaign = %+v, want original %+v", second, first)
	}
}

func TestCreateDeliveriesForActiveSubscribers(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	active, err := st.UpsertPendingSubscriber(ctx, "active@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber(active) error = %v", err)
	}
	_, err = st.CreateToken(ctx, active.ID, TokenConfirmSubscribe, "active-token", now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("CreateToken(active) error = %v", err)
	}
	if _, err := st.ActivateSubscriberByToken(ctx, "active-token", now.Add(time.Minute)); err != nil {
		t.Fatalf("ActivateSubscriberByToken(active) error = %v", err)
	}
	if _, err := st.UpsertPendingSubscriber(ctx, "pending@example.com", now); err != nil {
		t.Fatalf("UpsertPendingSubscriber(pending) error = %v", err)
	}
	campaign, _, err := st.CreateCampaignIfNew(ctx, CampaignInput{
		SourcePath:    "/news/a.html",
		ContentSHA256: "campaign-hash",
		Subject:       "Subject",
		FromName:      "Example",
		HTML:          []byte("<p>Hello</p>"),
	}, now)
	if err != nil {
		t.Fatalf("CreateCampaignIfNew() error = %v", err)
	}

	count, err := st.CreateDeliveriesForCampaign(ctx, campaign.ID, now)
	if err != nil {
		t.Fatalf("CreateDeliveriesForCampaign() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("delivery count = %d, want 1", count)
	}
}

func TestClaimPendingDeliverySkipsRetryExhausted(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	sub, err := st.UpsertPendingSubscriber(ctx, "active@example.com", now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	_, err = st.CreateToken(ctx, sub.ID, TokenConfirmSubscribe, "active-token", now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if _, err := st.ActivateSubscriberByToken(ctx, "active-token", now.Add(time.Minute)); err != nil {
		t.Fatalf("ActivateSubscriberByToken() error = %v", err)
	}
	campaign, _, err := st.CreateCampaignIfNew(ctx, CampaignInput{
		SourcePath:    "/news/a.html",
		ContentSHA256: "campaign-hash",
		Subject:       "Subject",
		FromName:      "Example",
		HTML:          []byte("<p>Hello</p>"),
	}, now)
	if err != nil {
		t.Fatalf("CreateCampaignIfNew() error = %v", err)
	}
	if _, err := st.CreateDeliveriesForCampaign(ctx, campaign.ID, now); err != nil {
		t.Fatalf("CreateDeliveriesForCampaign() error = %v", err)
	}
	job, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if !ok {
		t.Fatal("expected pending delivery")
	}
	if job.RecipientEmail != "active@example.com" || job.Campaign.Subject != "Subject" {
		t.Fatalf("job = %+v", job)
	}
	if err := st.MarkDeliveryFailed(ctx, job.Delivery.ID, "temporary error", 3, now); err != nil {
		t.Fatalf("MarkDeliveryFailed(1) error = %v", err)
	}
	if err := st.MarkDeliveryFailed(ctx, job.Delivery.ID, "temporary error", 3, now); err != nil {
		t.Fatalf("MarkDeliveryFailed(2) error = %v", err)
	}
	if err := st.MarkDeliveryFailed(ctx, job.Delivery.ID, "temporary error", 3, now); err != nil {
		t.Fatalf("MarkDeliveryFailed(3) error = %v", err)
	}

	_, ok, err = st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("second ClaimPendingDelivery() error = %v", err)
	}
	if ok {
		t.Fatal("retry-exhausted delivery should not be claimable")
	}
}

func openTestStore(t *testing.T, ctx context.Context) *SQLStore {
	t.Helper()
	st := openTestStoreAt(t, ctx, "sqlite://:memory:")
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return st
}

func openTestStoreAt(t *testing.T, ctx context.Context, databaseURL string) *SQLStore {
	t.Helper()
	st, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return st
}
