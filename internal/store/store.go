package store

import (
	"context"
	"time"
)

type Store interface {
	UpsertPendingSubscriber(ctx context.Context, email string, now time.Time) (Subscriber, error)
	FindSubscriberByEmail(ctx context.Context, email string) (Subscriber, bool, error)
	CreateToken(ctx context.Context, subscriberID string, purpose TokenPurpose, hash string, expiresAt time.Time, now time.Time) (Token, error)
	UseToken(ctx context.Context, hash string, purpose TokenPurpose, now time.Time) (Token, error)
	ActivateSubscriberByToken(ctx context.Context, hash string, now time.Time) (Subscriber, error)
	UnsubscribeSubscriberByToken(ctx context.Context, hash string, now time.Time) (Subscriber, error)
	CreateCampaignIfNew(ctx context.Context, input CampaignInput, now time.Time) (Campaign, bool, error)
	CreateDeliveriesForCampaign(ctx context.Context, campaignID string, now time.Time) (int, error)
	ClaimPendingDelivery(ctx context.Context, maxAttempts int, now time.Time) (DeliveryJob, bool, error)
	MarkDeliverySent(ctx context.Context, deliveryID string, now time.Time) error
	MarkDeliveryFailed(ctx context.Context, deliveryID string, lastError string, maxAttempts int, now time.Time) error
	Ping(ctx context.Context) error
}
