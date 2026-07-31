package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pmstowski/newsletter/internal/store"
)

func TestScanOnceCreatesCampaignAndDeliveriesForActiveSubscribers(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	activateSubscriber(t, ctx, st, "active@example.com", now)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "issue.html"), "---\nsubject: Issue One\n---\n<html><body>Hello</body></html>")

	if err := ScanOnce(ctx, st, dir, "Default Sender", testLogger()); err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}

	job, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if !ok {
		t.Fatal("expected pending delivery")
	}
	if job.Campaign.Subject != "Issue One" || job.RecipientEmail != "active@example.com" {
		t.Fatalf("job = %+v", job)
	}
}

func TestScanOnceSkipsDuplicateContentHash(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	activateSubscriber(t, ctx, st, "active@example.com", now)
	dir := t.TempDir()
	contents := "---\nsubject: Issue One\n---\n<html><body>Hello</body></html>"
	writeFile(t, filepath.Join(dir, "issue.html"), contents)

	if err := ScanOnce(ctx, st, dir, "Default Sender", testLogger()); err != nil {
		t.Fatalf("first ScanOnce() error = %v", err)
	}
	job, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if !ok {
		t.Fatal("expected first pending delivery")
	}
	if err := st.MarkDeliverySent(ctx, job.Delivery.ID, now); err != nil {
		t.Fatalf("MarkDeliverySent() error = %v", err)
	}
	writeFile(t, filepath.Join(dir, "copy.html"), contents)

	if err := ScanOnce(ctx, st, dir, "Default Sender", testLogger()); err != nil {
		t.Fatalf("second ScanOnce() error = %v", err)
	}
	_, ok, err = st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("second ClaimPendingDelivery() error = %v", err)
	}
	if ok {
		t.Fatal("duplicate content should not create another pending delivery")
	}
}

func TestScanOnceIgnoresNonHTMLFiles(t *testing.T) {
	ctx := context.Background()
	st := openWorkerStore(t, ctx)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	activateSubscriber(t, ctx, st, "active@example.com", now)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.txt"), "<html><body>Hello</body></html>")

	if err := ScanOnce(ctx, st, dir, "Default Sender", testLogger()); err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	_, ok, err := st.ClaimPendingDelivery(ctx, 3, now)
	if err != nil {
		t.Fatalf("ClaimPendingDelivery() error = %v", err)
	}
	if ok {
		t.Fatal("non-HTML file should not create a delivery")
	}
}

func openWorkerStore(t *testing.T, ctx context.Context) *store.SQLStore {
	t.Helper()
	st, err := store.Open(ctx, "sqlite://:memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return st
}

func activateSubscriber(t *testing.T, ctx context.Context, st *store.SQLStore, email string, now time.Time) {
	t.Helper()
	sub, err := st.UpsertPendingSubscriber(ctx, email, now)
	if err != nil {
		t.Fatalf("UpsertPendingSubscriber() error = %v", err)
	}
	if _, err := st.CreateToken(ctx, sub.ID, store.TokenConfirmSubscribe, email+"-token", now.Add(time.Hour), now); err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if _, err := st.ActivateSubscriberByToken(ctx, email+"-token", now.Add(time.Minute)); err != nil {
		t.Fatalf("ActivateSubscriberByToken() error = %v", err)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
