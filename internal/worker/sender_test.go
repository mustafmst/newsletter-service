package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/store"
)

func TestSendOneSendsPendingDeliveryAndMarksSent(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	createPendingDelivery(t, ctx, st, now)
	sender := &workerFakeSender{}

	sent, err := SendOne(ctx, st, sender, 0, 3, testLogger())
	if err != nil {
		t.Fatalf("SendOne() error = %v", err)
	}
	if !sent {
		t.Fatal("expected a send attempt")
	}
	if len(sender.messages) != 1 {
		t.Fatalf("messages = %d", len(sender.messages))
	}
	if sender.messages[0].Subject != "Issue One" || sender.messages[0].ToEmail != "active@example.com" {
		t.Fatalf("message = %+v", sender.messages[0])
	}
	_, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if ok {
		t.Fatal("sent delivery should not remain pending")
	}
}

func TestSendOneRecordsSenderErrorForRetry(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	createPendingDelivery(t, ctx, st, now)
	sender := &workerFakeSender{err: errors.New("smtp unavailable")}

	sent, err := SendOne(ctx, st, sender, 0, 3, testLogger())
	if err != nil {
		t.Fatalf("SendOne() error = %v", err)
	}
	if !sent {
		t.Fatal("expected attempted send")
	}
	job, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if !ok {
		t.Fatal("failed delivery should remain pending before max attempts")
	}
	if job.Delivery.AttemptCount != 1 || job.Delivery.LastError != "smtp unavailable" {
		t.Fatalf("delivery = %+v", job.Delivery)
	}
}

func TestSendOneStopsClaimingAfterMaxAttempts(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	createPendingDelivery(t, ctx, st, now)
	sender := &workerFakeSender{err: errors.New("smtp unavailable")}

	for i := 0; i < 3; i++ {
		if _, err := SendOne(ctx, st, sender, 0, 3, testLogger()); err != nil {
			t.Fatalf("SendOne(%d) error = %v", i, err)
		}
	}
	_, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if ok {
		t.Fatal("retry-exhausted delivery should not be claimable")
	}
}

func createPendingDelivery(t *testing.T, ctx context.Context, st *store.SQLStore, now time.Time) {
	t.Helper()
	activateSubscriber(t, ctx, st, "active@example.com", now)
	campaign, _, err := st.CreateCampaignIfNew(ctx, store.CampaignInput{
		SourcePath:    "/news/issue.html",
		ContentSHA256: "issue-hash",
		Subject:       "Issue One",
		FromName:      "Example",
		HTML:          []byte("<p>Hello</p>"),
	}, now)
	if err != nil {
		t.Fatalf("CreateCampaignIfNew() error = %v", err)
	}
	if _, err := st.CreateDeliveriesForCampaign(ctx, campaign.ID, now); err != nil {
		t.Fatalf("CreateDeliveriesForCampaign() error = %v", err)
	}
}

type workerFakeSender struct {
	err      error
	messages []mailer.Message
}

func (f *workerFakeSender) Send(ctx context.Context, msg mailer.Message) error {
	f.messages = append(f.messages, msg)
	return f.err
}
