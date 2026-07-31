package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var ErrNotFound = errors.New("not found")

type SQLStore struct {
	db     *sqlx.DB
	driver string
}

func Open(ctx context.Context, databaseURL string) (*SQLStore, error) {
	driver, dsn, err := driverAndDSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sqlx.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLStore{db: db, driver: driver}, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) Migrate(ctx context.Context) error {
	contents, err := migrationFiles.ReadFile("migrations/001_init.sql")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, string(contents))
	return err
}

func (s *SQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *SQLStore) UpsertPendingSubscriber(ctx context.Context, email string, now time.Time) (Subscriber, error) {
	id, err := newID()
	if err != nil {
		return Subscriber{}, err
	}
	ts := formatTime(now)
	query := `
INSERT INTO subscribers (id, email, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(email) DO UPDATE SET
	status = CASE WHEN subscribers.status = 'active' THEN subscribers.status ELSE excluded.status END,
	updated_at = excluded.updated_at,
	unsubscribed_at = CASE WHEN subscribers.status = 'active' THEN subscribers.unsubscribed_at ELSE NULL END
RETURNING id, email, status, created_at, updated_at, confirmed_at, unsubscribed_at`
	var sub Subscriber
	err = s.db.QueryRowxContext(ctx, s.rebind(query), id, email, string(SubscriberPending), ts, ts).Scan(
		&sub.ID, &sub.Email, &sub.Status, scanTime(&sub.CreatedAt), scanTime(&sub.UpdatedAt), scanNullTime(&sub.ConfirmedAt), scanNullTime(&sub.UnsubscribedAt),
	)
	return sub, err
}

func (s *SQLStore) FindSubscriberByEmail(ctx context.Context, email string) (Subscriber, bool, error) {
	var sub Subscriber
	err := s.db.QueryRowxContext(ctx, s.rebind(`
SELECT id, email, status, created_at, updated_at, confirmed_at, unsubscribed_at
FROM subscribers
WHERE email = ?`), email).Scan(
		&sub.ID, &sub.Email, &sub.Status, scanTime(&sub.CreatedAt), scanTime(&sub.UpdatedAt), scanNullTime(&sub.ConfirmedAt), scanNullTime(&sub.UnsubscribedAt),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscriber{}, false, nil
	}
	if err != nil {
		return Subscriber{}, false, err
	}
	return sub, true, nil
}

func (s *SQLStore) CreateToken(ctx context.Context, subscriberID string, purpose TokenPurpose, hash string, expiresAt time.Time, now time.Time) (Token, error) {
	id, err := newID()
	if err != nil {
		return Token{}, err
	}
	tok := Token{
		ID:           id,
		SubscriberID: subscriberID,
		Purpose:      purpose,
		Hash:         hash,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}
	_, err = s.db.ExecContext(ctx, s.rebind(`
INSERT INTO tokens (id, subscriber_id, purpose, hash, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?)`), tok.ID, tok.SubscriberID, string(tok.Purpose), tok.Hash, formatTime(tok.ExpiresAt), formatTime(tok.CreatedAt))
	return tok, err
}

func (s *SQLStore) UseToken(ctx context.Context, hash string, purpose TokenPurpose, now time.Time) (Token, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return Token{}, err
	}
	defer rollback(tx)
	tok, err := s.useTokenTx(ctx, tx, hash, purpose, now)
	if err != nil {
		return Token{}, err
	}
	if err := tx.Commit(); err != nil {
		return Token{}, err
	}
	return tok, nil
}

func (s *SQLStore) ActivateSubscriberByToken(ctx context.Context, hash string, now time.Time) (Subscriber, error) {
	return s.updateSubscriberByToken(ctx, hash, TokenConfirmSubscribe, SubscriberActive, true, now)
}

func (s *SQLStore) UnsubscribeSubscriberByToken(ctx context.Context, hash string, now time.Time) (Subscriber, error) {
	return s.updateSubscriberByToken(ctx, hash, TokenConfirmUnsubscribe, SubscriberUnsubscribed, false, now)
}

func (s *SQLStore) CreateCampaignIfNew(ctx context.Context, input CampaignInput, now time.Time) (Campaign, bool, error) {
	id, err := newID()
	if err != nil {
		return Campaign{}, false, err
	}
	ts := formatTime(now)
	res, err := s.db.ExecContext(ctx, s.rebind(`
INSERT INTO campaigns (id, source_path, content_sha256, subject, from_name, html, status, created_at, processed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(content_sha256) DO NOTHING`), id, input.SourcePath, input.ContentSHA256, input.Subject, input.FromName, input.HTML, "processed", ts, ts)
	if err != nil {
		return Campaign{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Campaign{}, false, err
	}
	campaign, err := s.findCampaignByHash(ctx, input.ContentSHA256)
	if err != nil {
		return Campaign{}, false, err
	}
	return campaign, affected > 0, nil
}

func (s *SQLStore) CreateDeliveriesForCampaign(ctx context.Context, campaignID string, now time.Time) (int, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	var subscribers []Subscriber
	rows, err := tx.QueryxContext(ctx, s.rebind(`
SELECT id, email, status, created_at, updated_at, confirmed_at, unsubscribed_at
FROM subscribers
WHERE status = ?
ORDER BY created_at ASC`), string(SubscriberActive))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var sub Subscriber
		if err := rows.Scan(&sub.ID, &sub.Email, &sub.Status, scanTime(&sub.CreatedAt), scanTime(&sub.UpdatedAt), scanNullTime(&sub.ConfirmedAt), scanNullTime(&sub.UnsubscribedAt)); err != nil {
			return 0, err
		}
		subscribers = append(subscribers, sub)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	for _, sub := range subscribers {
		id, err := newID()
		if err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, s.rebind(`
INSERT INTO deliveries (id, campaign_id, subscriber_id, recipient_email, status, attempt_count, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 0, ?, ?)
ON CONFLICT(campaign_id, subscriber_id) DO NOTHING`), id, campaignID, sub.ID, sub.Email, string(DeliveryPending), formatTime(now), formatTime(now))
		if err != nil {
			return 0, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		count += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLStore) ClaimPendingDelivery(ctx context.Context, maxAttempts int, now time.Time) (DeliveryJob, bool, error) {
	var job DeliveryJob
	err := s.db.QueryRowxContext(ctx, s.rebind(`
SELECT
	d.id, d.campaign_id, d.subscriber_id, d.recipient_email, d.status, d.attempt_count, d.last_error, d.created_at, d.updated_at, d.sent_at,
	c.id, c.source_path, c.content_sha256, c.subject, c.from_name, c.html, c.status, c.created_at, c.processed_at
FROM deliveries d
JOIN campaigns c ON c.id = d.campaign_id
WHERE d.status = ? AND d.attempt_count < ?
ORDER BY d.created_at ASC
LIMIT 1`), string(DeliveryPending), maxAttempts).Scan(
		&job.Delivery.ID, &job.Delivery.CampaignID, &job.Delivery.SubscriberID, &job.Delivery.RecipientEmail, &job.Delivery.Status, &job.Delivery.AttemptCount, &job.Delivery.LastError, scanTime(&job.Delivery.CreatedAt), scanTime(&job.Delivery.UpdatedAt), scanNullTime(&job.Delivery.SentAt),
		&job.Campaign.ID, &job.Campaign.SourcePath, &job.Campaign.ContentSHA256, &job.Campaign.Subject, &job.Campaign.FromName, &job.Campaign.HTML, &job.Campaign.Status, scanTime(&job.Campaign.CreatedAt), scanTime(&job.Campaign.ProcessedAt),
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryJob{}, false, nil
	}
	if err != nil {
		return DeliveryJob{}, false, err
	}
	job.RecipientEmail = job.Delivery.RecipientEmail
	return job, true, nil
}

func (s *SQLStore) MarkDeliverySent(ctx context.Context, deliveryID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
UPDATE deliveries
SET status = ?, updated_at = ?, sent_at = ?
WHERE id = ?`), string(DeliverySent), formatTime(now), formatTime(now), deliveryID)
	return err
}

func (s *SQLStore) MarkDeliveryFailed(ctx context.Context, deliveryID string, lastError string, maxAttempts int, now time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
UPDATE deliveries
SET attempt_count = attempt_count + 1,
	last_error = ?,
	status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
	updated_at = ?
WHERE id = ?`), lastError, maxAttempts, string(DeliveryFailed), string(DeliveryPending), formatTime(now), deliveryID)
	return err
}

func (s *SQLStore) updateSubscriberByToken(ctx context.Context, hash string, purpose TokenPurpose, status SubscriberStatus, confirmed bool, now time.Time) (Subscriber, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return Subscriber{}, err
	}
	defer rollback(tx)
	tok, err := s.useTokenTx(ctx, tx, hash, purpose, now)
	if err != nil {
		return Subscriber{}, err
	}
	query := `
UPDATE subscribers
SET status = ?, updated_at = ?, confirmed_at = CASE WHEN ? THEN ? ELSE confirmed_at END, unsubscribed_at = CASE WHEN ? THEN updated_at ELSE ? END
WHERE id = ?`
	_, err = tx.ExecContext(ctx, s.rebind(query), string(status), formatTime(now), confirmed, formatTime(now), status == SubscriberUnsubscribed, formatTime(now), tok.SubscriberID)
	if err != nil {
		return Subscriber{}, err
	}
	sub, err := scanSubscriber(tx.QueryRowxContext(ctx, s.rebind(`
SELECT id, email, status, created_at, updated_at, confirmed_at, unsubscribed_at
FROM subscribers
WHERE id = ?`), tok.SubscriberID))
	if err != nil {
		return Subscriber{}, err
	}
	if err := tx.Commit(); err != nil {
		return Subscriber{}, err
	}
	return sub, nil
}

func (s *SQLStore) useTokenTx(ctx context.Context, tx *sqlx.Tx, hash string, purpose TokenPurpose, now time.Time) (Token, error) {
	tok, err := scanToken(tx.QueryRowxContext(ctx, s.rebind(`
SELECT id, subscriber_id, purpose, hash, expires_at, created_at, used_at
FROM tokens
WHERE hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?`), hash, string(purpose), formatTime(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	_, err = tx.ExecContext(ctx, s.rebind(`UPDATE tokens SET used_at = ? WHERE id = ?`), formatTime(now), tok.ID)
	if err != nil {
		return Token{}, err
	}
	tok.UsedAt = &now
	return tok, nil
}

func (s *SQLStore) findCampaignByHash(ctx context.Context, hash string) (Campaign, error) {
	return scanCampaign(s.db.QueryRowxContext(ctx, s.rebind(`
SELECT id, source_path, content_sha256, subject, from_name, html, status, created_at, processed_at
FROM campaigns
WHERE content_sha256 = ?`), hash))
}

func (s *SQLStore) rebind(query string) string {
	return s.db.Rebind(query)
}

func driverAndDSN(databaseURL string) (string, string, error) {
	switch {
	case strings.HasPrefix(databaseURL, "sqlite://"):
		return "sqlite", strings.TrimPrefix(databaseURL, "sqlite://"), nil
	case strings.HasPrefix(databaseURL, "postgres://"), strings.HasPrefix(databaseURL, "postgresql://"):
		return "postgres", databaseURL, nil
	default:
		return "", "", fmt.Errorf("unsupported DATABASE_URL scheme")
	}
}

func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, raw)
}

func scanSubscriber(row interface {
	Scan(dest ...any) error
}) (Subscriber, error) {
	var sub Subscriber
	err := row.Scan(&sub.ID, &sub.Email, &sub.Status, scanTime(&sub.CreatedAt), scanTime(&sub.UpdatedAt), scanNullTime(&sub.ConfirmedAt), scanNullTime(&sub.UnsubscribedAt))
	return sub, err
}

func scanToken(row interface {
	Scan(dest ...any) error
}) (Token, error) {
	var tok Token
	err := row.Scan(&tok.ID, &tok.SubscriberID, &tok.Purpose, &tok.Hash, scanTime(&tok.ExpiresAt), scanTime(&tok.CreatedAt), scanNullTime(&tok.UsedAt))
	return tok, err
}

func scanCampaign(row interface {
	Scan(dest ...any) error
}) (Campaign, error) {
	var campaign Campaign
	err := row.Scan(&campaign.ID, &campaign.SourcePath, &campaign.ContentSHA256, &campaign.Subject, &campaign.FromName, &campaign.HTML, &campaign.Status, scanTime(&campaign.CreatedAt), scanTime(&campaign.ProcessedAt))
	return campaign, err
}

func scanTime(target *time.Time) sql.Scanner {
	return timeScanner{target: target}
}

func scanNullTime(target **time.Time) sql.Scanner {
	return nullTimeScanner{target: target}
}

type timeScanner struct {
	target *time.Time
}

func (s timeScanner) Scan(value any) error {
	switch typed := value.(type) {
	case string:
		parsed, err := parseTime(typed)
		if err != nil {
			return err
		}
		*s.target = parsed
	case []byte:
		parsed, err := parseTime(string(typed))
		if err != nil {
			return err
		}
		*s.target = parsed
	case time.Time:
		*s.target = typed
	default:
		return fmt.Errorf("unsupported time value %T", value)
	}
	return nil
}

type nullTimeScanner struct {
	target **time.Time
}

func (s nullTimeScanner) Scan(value any) error {
	if value == nil {
		*s.target = nil
		return nil
	}
	var parsed time.Time
	if err := (timeScanner{target: &parsed}).Scan(value); err != nil {
		return err
	}
	*s.target = &parsed
	return nil
}

func rollback(tx *sqlx.Tx) {
	_ = tx.Rollback()
}
