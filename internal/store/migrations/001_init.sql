CREATE TABLE IF NOT EXISTS subscribers (
	id TEXT PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	confirmed_at TEXT,
	unsubscribed_at TEXT
);

CREATE TABLE IF NOT EXISTS tokens (
	id TEXT PRIMARY KEY,
	subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
	purpose TEXT NOT NULL,
	hash TEXT NOT NULL UNIQUE,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL,
	used_at TEXT
);

CREATE TABLE IF NOT EXISTS campaigns (
	id TEXT PRIMARY KEY,
	source_path TEXT NOT NULL,
	content_sha256 TEXT NOT NULL UNIQUE,
	subject TEXT NOT NULL,
	from_name TEXT NOT NULL,
	html BLOB NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	processed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS deliveries (
	id TEXT PRIMARY KEY,
	campaign_id TEXT NOT NULL REFERENCES campaigns(id),
	subscriber_id TEXT NOT NULL REFERENCES subscribers(id),
	recipient_email TEXT NOT NULL,
	status TEXT NOT NULL,
	attempt_count INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	sent_at TEXT,
	UNIQUE(campaign_id, subscriber_id)
);

CREATE INDEX IF NOT EXISTS idx_tokens_hash_purpose ON tokens(hash, purpose);
CREATE INDEX IF NOT EXISTS idx_deliveries_pending ON deliveries(status, attempt_count, created_at);
