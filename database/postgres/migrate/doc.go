// Package migrate wraps golang-migrate/migrate/v4 into the convention this
// platform standardized on for Go-owned schema evolution (ROADMAP.md PH-3
// W8, GO-STANDARDS-ROLLOUT_PLAN.md R1):
//
//   - SQL-first paired files, embedded in the service binary via embed.FS:
//     migrations/0001_title.up.sql
//     migrations/0001_title.down.sql
//     Zero-padded sequential versions, not timestamps, so ordering is
//     reviewable in a diff.
//
//   - The Go migration system owns the schema. The schema that exists today
//     is the platform's baseline, captured as each service's migration 0001,
//     written idempotently (CREATE TABLE IF NOT EXISTS, ...) so it is a no-op
//     against an existing database and creates from scratch on a greenfield
//     one. Every change from there is a new versioned migration. Config.Mode
//     gates this: ModeMigrate applies pending migrations; ModeNone is a no-op
//     for environments where the schema is provisioned out-of-band.
//
//   - Schema-agnostic migrations; config-driven search_path. Migration files
//     never contain SET search_path or schema-qualified names — they use bare
//     identifiers and land in whatever schema the connection's search_path
//     selects. The active schema is decided by configuration alone:
//     Config.SearchPath here and client.Config.SearchPath on the app pool,
//     set to the SAME value, so migrations and the application always agree.
//     Changing schema is a config change, never a migration edit.
//
//   - Per-service migrations table. Multiple services share the interim
//     iudx_db, so each Run call passes its own history table via
//     Config.TableName (schema_migrations_<service>) — the equivalent of
//     the DSN's x-migrations-table option, expressed through
//     golang-migrate's Config.MigrationsTable instead.
//
//   - Dirty-state handling is loud. If a migration fails partway, golang-migrate
//     marks the tracked version dirty and refuses to run anything further.
//     Run surfaces that as a *DirtyStateError naming the exact version — do
//     not swallow it and boot anyway. Recovery: fix the underlying issue by
//     hand, then run `migrate force <version>` (via a one-off using the same
//     Config) before restarting the service.
//
//   - Zero-downtime pattern (normative): expand → migrate → contract.
//     Additive DDL first (new column/table/index CONCURRENTLY), ship code
//     that writes-both/reads-new, backfill in batches, only then a later
//     contract migration drops the old shape. Never rename/retype a column
//     in place — a rolling deploy needs the previous binary to keep working
//     against the expanded (not yet contracted) schema.
//
//   - Deployment ordering: Run belongs in an init step before the service
//     starts serving traffic (a Job/initContainer, or a synchronous call at
//     the top of main before the router is wired) — never triggered lazily
//     on first request.
package migrate
