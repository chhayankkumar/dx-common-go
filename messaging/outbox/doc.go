// Package outbox is the promoted, generic form of the transactional-outbox
// pattern dx-acl-go pioneered service-locally (policy_outbox +
// OutboxDispatcher). It guarantees a domain write and its downstream event
// are never split by a crash or a RabbitMQ outage: the event row is written
// in the same transaction as the domain row, and a background Dispatcher
// drains unsent rows to the broker, retrying until it succeeds.
//
// A consuming service owns its own outbox table (via its own
// database/postgres/migrate migration — this package only talks SQL against
// a table name the caller supplies, it doesn't create one) with this shape:
//
//	CREATE TABLE IF NOT EXISTS <table> (
//	    id          uuid PRIMARY KEY,
//	    action      text NOT NULL,
//	    payload     jsonb NOT NULL,
//	    request_id  text,
//	    created_at  timestamptz NOT NULL DEFAULT now(),
//	    sent_at     timestamptz,
//	    attempts    integer NOT NULL DEFAULT 0
//	);
//	CREATE INDEX IF NOT EXISTS idx_<table>_unsent ON <table> (created_at) WHERE sent_at IS NULL;
//
// Usage: insert via Store.Insert inside the same pgx.Tx as the domain write
// (so both commit or both roll back together); run a Dispatcher in the
// background, calling Kick() after a write for low-latency delivery instead
// of waiting for the next poll tick.
package outbox
