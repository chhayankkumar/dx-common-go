-- Shared fixture schema for dx-common-go's own real-Postgres integration
-- tests (dao/repository/sqlcx packages) — not a service migration. Applied
-- via dxtest/containers.WithSetupSQL, which runs this file on every
-- Postgres(t, ...) call in a test binary, so every statement here must be
-- idempotent (IF NOT EXISTS only, no seed data).

CREATE TABLE IF NOT EXISTS categories (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS widgets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'ACTIVE',
    category_id TEXT REFERENCES categories(id),
    quantity    INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    version     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT,
    updated_by  TEXT,
    deleted_at  TIMESTAMPTZ
);
