package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/pmstowski/newsletter/internal/mailer"
	"github.com/pmstowski/newsletter/internal/store"
)

func SendOne(ctx context.Context, st store.Store, sender mailer.Sender, delay time.Duration, maxAttempts int, logger *slog.Logger) (bool, error) {
	now := time.Now().UTC()
	job, ok, err := st.ClaimPendingDelivery(ctx, maxAttempts, now)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	msg := mailer.NewNewsletterMessage("", job.Campaign.FromName, job.RecipientEmail, job.Campaign.Subject, job.Campaign.HTML)
	if err := sender.Send(ctx, msg); err != nil {
		if markErr := st.MarkDeliveryFailed(ctx, job.Delivery.ID, err.Error(), maxAttempts, time.Now().UTC()); markErr != nil {
			return true, markErr
		}
		logger.Warn("newsletter delivery failed", "delivery_id", job.Delivery.ID, "error", err)
		if delay > 0 {
			sleep(ctx, delay)
		}
		return true, nil
	}
	if err := st.MarkDeliverySent(ctx, job.Delivery.ID, time.Now().UTC()); err != nil {
		return true, err
	}
	logger.Info("newsletter delivery sent", "delivery_id", job.Delivery.ID, "recipient", job.RecipientEmail)
	if delay > 0 {
		sleep(ctx, delay)
	}
	return true, nil
}

func RunSender(ctx context.Context, interval time.Duration, sendOne func(context.Context) (bool, error), logger *slog.Logger) {
	for {
		sent, err := sendOne(ctx)
		if err != nil {
			logger.Error("newsletter sender failed", "error", err)
		}
		wait := interval
		if sent {
			wait = 0
		}
		if wait <= 0 {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func sleep(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
