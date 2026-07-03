# Changelog

Not yet tagged/released (see ROADMAP.md PH-3 W11 / GO-STANDARDS-ROLLOUT_PLAN.md Q10 for the
release-engineering plan — tagging wasn't in scope for the change below). Consumers use the
`replace github.com/datakaveri/dx-common-go => ../dx-common-go` directive, so changes here are
picked up immediately by every service without a version bump.

## Unreleased

### Added
- `database/postgres/migrate` — `golang-migrate`-based migration runner. `Config{Mode, DSN,
  TableName}`, `Run`, `Status`, loud `*DirtyStateError` on a dirty schema history table. Reference
  implementation: `dx-acl-go`.
- `database/postgres`: `InTransaction`/`TxFromContext` (ambient, context-propagated transactions —
  nested `InTransaction` calls join the outer transaction instead of opening a second one),
  `WithRetryableTx` (retries on Postgres `40001`/`40P01`), `WithAdvisoryLock`.
- `database/postgres/dao`: `BaseDAO.CopyFrom`, `InsertMany`, `UpdateVersioned` (optimistic locking
  via a version column, `ErrStaleVersion`), `InsertIgnore` (`INSERT ... ON CONFLICT DO NOTHING`),
  `WithSoftDeleteFilter`/`Unscoped()` (opt-in auto-exclusion of soft-deleted rows),
  `NewBaseDAOWith`/`WithIDColumn` (options-taking constructor, kept separate from `NewBaseDAO` so
  every existing 2-argument call site is untouched).
- `messaging/outbox` (new package) — generic transactional-outbox `Store`/`Dispatcher`, promoted
  from `dx-acl-go`'s service-local `policy_outbox` implementation.
- `messaging/rabbitmq`: `DeclareQueueWithDLQ` (topic-DLX topology helper), `ConsumerRunner.MaxAttempts`
  (caps infinite requeue loops on a poison message; process-local, resets on reconnect) and `Dedup`
  wiring, `ConsumerRunner.Stop`.
- `database/redis`: `Mutex` (single-node distributed lock, not Redlock-safe across multiple Redis
  nodes), `Allow` (fixed-window rate limiter), `GetOrSet` (cache-aside helper).
- `database/elastic`: typed `Repo[T]` (`Search`/`SearchAfter`/`Get`/`Index`/`BulkIndex`),
  `EnsureAlias`/`SwapAlias`/`PutMapping`/`Reindex` (index/alias management), `BulkIndexWithRetry`
  (retry + per-item error stats).

### Fixed
- `database/elastic.CreateIndex(ctx, index, nil)` sent a literal 4-byte `"null"` request body
  instead of an empty `{}` — a nil `map[string]any` boxed into an `any` parameter is a non-nil
  interface holding a nil map, so the existing `body != nil` check didn't catch it. Elasticsearch
  rejected the malformed body with a `not_x_content_exception`. Found via a real-Elasticsearch
  integration test (`database/elastic/integration_test.go`, `ES_TEST_ADDR`-gated).
