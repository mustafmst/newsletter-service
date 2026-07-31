package store

import "time"

type SubscriberStatus string

const (
	SubscriberPending      SubscriberStatus = "pending"
	SubscriberActive       SubscriberStatus = "active"
	SubscriberUnsubscribed SubscriberStatus = "unsubscribed"
)

type TokenPurpose string

const (
	TokenConfirmSubscribe   TokenPurpose = "confirm_subscribe"
	TokenConfirmUnsubscribe TokenPurpose = "confirm_unsubscribe"
)

type DeliveryStatus string

const (
	DeliveryPending DeliveryStatus = "pending"
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
)

type Subscriber struct {
	ID             string
	Email          string
	Status         SubscriberStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ConfirmedAt    *time.Time
	UnsubscribedAt *time.Time
}

type Token struct {
	ID           string
	SubscriberID string
	Purpose      TokenPurpose
	Hash         string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UsedAt       *time.Time
}

type Campaign struct {
	ID            string
	SourcePath    string
	ContentSHA256 string
	Subject       string
	FromName      string
	HTML          []byte
	Status        string
	CreatedAt     time.Time
	ProcessedAt   time.Time
}

type CampaignInput struct {
	SourcePath    string
	ContentSHA256 string
	Subject       string
	FromName      string
	HTML          []byte
}

type Delivery struct {
	ID             string
	CampaignID     string
	SubscriberID   string
	RecipientEmail string
	Status         DeliveryStatus
	AttemptCount   int
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SentAt         *time.Time
}

type DeliveryJob struct {
	Delivery       Delivery
	Campaign       Campaign
	RecipientEmail string
}
