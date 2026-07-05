-- The minimal service owns these two tables outright (unlike a legacy-DB
-- service, whose interim tables stay Flyway-owned). golang-migrate applies
-- this pair; keep it idempotent so an already-provisioned dev DB adopts the
-- history table cleanly.

CREATE TABLE IF NOT EXISTS widgets (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The transactional-outbox table, shaped exactly as outbox.PGStore expects
-- (id, action, payload, request_id, created_at, attempts, sent_at).
CREATE TABLE IF NOT EXISTS widget_outbox (
    id         UUID PRIMARY KEY,
    action     TEXT NOT NULL,
    payload    BYTEA NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts   INTEGER NOT NULL DEFAULT 0,
    sent_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS widget_outbox_unsent_idx
    ON widget_outbox (created_at) WHERE sent_at IS NULL;
