// Package migrate wraps golang-migrate/migrate/v4 into the convention this
// platform standardized on for Go-owned schema evolution (ROADMAP.md PH-3
// W8, GO-STANDARDS-ROLLOUT_PLAN.md R1):
//
//   - SQL-first paired files, embedded in the service binary via embed.FS:
//     migrations/0001_title.up.sql
//     migrations/0001_title.down.sql
//     Zero-padded sequential versions (Flyway-style), not timestamps, so
//     ordering is reviewable in a diff.
//
//   - Legacy tables stay Flyway's. The interim state (before a service gets
//     its own database) is: legacy tables are read/written by Go but never
//     created or altered by Go — call Run with Config.Mode=ModeNone for
//     those. ModeMigrate is only for a service's own net-new tables (e.g.
//     acl's policy_outbox, an audit dedup index).
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
