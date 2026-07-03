# `database/elastic` — the shared Elasticsearch module

Generic, production-grade Elasticsearch infrastructure for every DX Go service: one configurable client, a composable query DSL, a typed per-index repository, index-lifecycle admin, bulk operations, health checks, and built-in observability. **Infrastructure only** — no service-specific logic lives here; mappings, index names, and search behaviour belong to the consuming service.

Reference consumer: `dx-catalogue-go` (`internal/elasticrepo` wraps `Repo[domain.Item]`, business queries stay in its `internal/query`).

---

## Adopting it in a service

```go
import dxelastic "github.com/datakaveri/dx-common-go/database/elastic"

es, err := dxelastic.NewClient(cfg.Elastic)        // pings at startup; config errors fail fast
repo := dxelastic.NewRepo[domain.Thing](es, cfg.Index)

// health wiring (dx-common-go/health):
hh.Register("elasticsearch", health.NewCustomChecker("elasticsearch", es.HealthCheck))
```

```yaml
# configs/config.yaml
elastic:
  addresses: ["http://elasticsearch:9200"]
  timeout: 10s
  # username / password / api_key          — auth (pick one scheme)
  # ca_cert_path: /etc/ssl/es-ca.pem       — private-CA TLS
  # insecure_skip_verify: true             — dev/test ONLY
  # max_retries: 3 / disable_retry: true   — transport retry policy
  # enable_metrics: true                   — Prometheus dx_elastic_* metrics
```

That's the whole integration: config → client → typed repo → health check.

## Configuration reference

| Key | Default | Notes |
|-----|---------|-------|
| `addresses` | — (required) | node URLs |
| `username` / `password` / `api_key` | — | basic auth or API key |
| `timeout` | `10s` | per-request bound |
| `ca_cert_path` | — | PEM bundle for private-CA clusters |
| `insecure_skip_verify` | `false` | dev/test only; never production |
| `max_retries` | `3` | transport retries on 429/502/503/504, exponential backoff |
| `disable_retry` | `false` | turn retries off (e.g. non-idempotent scripted updates) |
| `enable_metrics` | `false` | `dx_elastic_requests_total{method,status}` + `dx_elastic_request_duration_seconds{method}` on the default registry |
| `Logger` (runtime) | — | zap logger: Debug per request, Warn on failures |
| `Transport` (runtime) | — | custom `http.RoundTripper` — the test-mock / instrumentation seam; when set, TLS fields are ignored |

## What's where

| Concern | API |
|---------|-----|
| Documents | `IndexDoc · GetDoc · UpdateDoc · ScriptUpdate · UpdateByQuery · DeleteDoc · DeleteByQuery` |
| Search | `Search(index, SearchRequest{Query, Size/From, Sort, SearchAfter, SourceIncludes/Excludes, Aggregations, TrackTotalHits, Highlight})` · `Count` · `HitsAs[T]` |
| Typed repo | `Repo[T]`: `Get · Index · Search · SearchAfter · BulkIndex` — the common single-index case; drop to `*Client` for aggregations/highlights/scripts |
| Query DSL | `Match · MatchFuzzy · MatchPhrase · MultiMatch · MatchBoolPrefix · Term/Terms · Exists · Wildcard · Prefix · IDs · QueryString · Range · Nested · Bool()… · Geo{BoundingBox,Distance,Shape} · ScriptScore`; aggs: `TermsAgg · MetricAgg · DateHistogramAgg · FilterAgg · .Sub` |
| Bulk | `BulkDo(index, []BulkOp{IndexOp/UpdateOp/DeleteOp}, attempts)` → `BulkStats` (per-item errors, transport-level retry with backoff); `BulkIndexWithRetry` convenience |
| Index lifecycle | `EnsureIndex · CreateIndex · DeleteIndex · IndexExists · PutMapping · EnsureAlias · SwapAlias · AliasIndices · Reindex · PutIndexTemplate · DeleteIndexTemplate` |
| Health | `ClusterHealth` · `HealthCheck` (nil for green/**yellow** — single-node dev is always yellow; error for red/unreachable) |
| Errors | non-2xx → `dxerrors` (`NotFound`, `Validation`, `Conflict`, `Internal`) — handlers translate uniformly |

## Best practices

1. **Address data through an alias, never a physical index** (`iudx-docs` → `iudx-docs-v1`). Breaking mapping change = `CreateIndex(v2)` → `Reindex` → `SwapAlias` → `DeleteIndex(v1)`. Expand-only changes = `PutMapping`. (DATABASE.md §8.4 in `cdpg-claude/claude-docs`.)
2. **Provision, don't migrate, at boot**: `EnsureIndex` creates-if-absent and never touches an existing index. While a service shares a legacy index, the legacy owner keeps the mapping (dev provisioning is `es-init`).
3. **Deep pagination** past 10 000 results uses `SearchAfter` with a deterministic sort — never large `From` offsets.
4. **Bulk**: treat `BulkStats.Errors` as data, not an exception — a batch routinely partially succeeds. Retries only re-run the batch on transport failure, so make bulk writes idempotent (stable ids).
5. **Retries are on by default** (429/502/503/504). Disable them (`disable_retry`) for requests that must not replay.
6. **Highlighting** is request-level (`SearchRequest.Highlight`) and returns per-hit fragments in `Hit.Highlight` — use `Client.Search` (the typed `Repo` deliberately returns only `_source`).

## Observability & extension points

- `enable_metrics: true` + `Logger` give metrics/logging with zero call-site changes; both are implemented as a `RoundTripper` wrapper.
- The same **`Transport` seam** is where OpenTelemetry tracing plugs in later (ROADMAP PH-2) — wrap, don't fork.
- The DSL types are open maps (`Query`, `Agg`), so any ES feature the helpers don't cover can be expressed inline without waiting for a library change.

## Testing

- **Unit / mock** (no ES): either point `NewClient` at an `httptest.Server`, or inject a fake via `Config.Transport`. Every mocked response **must set the `X-Elastic-Product: Elasticsearch` header** — the official client refuses to talk to anything else. See `client_test.go` for both patterns.
- **Integration** (real ES): tests are gated on `ES_TEST_ADDR` (skip otherwise), e.g. `ES_TEST_ADDR=http://localhost:9200 go test ./database/elastic/...` against the dev stack's Elasticsearch. See `integration_test.go`.
