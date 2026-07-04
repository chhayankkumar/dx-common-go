# `database/elastic` — the shared Elasticsearch framework

Production-grade, generic Elasticsearch infrastructure for every DX Go service: configurable client, fluent query DSL, typed repositories, mapping framework, index-lifecycle orchestration, bulk operations, suggesters, scroll/PIT, vector/hybrid search readiness, health checks, and built-in observability. **Infrastructure only** — mappings, index names, and search behaviour belong to the consuming service.

Reference consumer: `dx-catalogue-go` (`internal/elasticrepo` wraps `Repo[domain.Item]`; business queries stay in its `internal/query`).

---

## 1. Adopting it in a service

```go
import dxelastic "github.com/datakaveri/dx-common-go/database/elastic"

es, err := dxelastic.NewClient(cfg.Elastic)          // pings at startup — config errors fail fast
repo := dxelastic.NewRepo[domain.Thing](es, "things") // typed repository over one index/alias
hh.Register("elasticsearch", health.NewCustomChecker("elasticsearch", es.HealthCheck))
```

```yaml
elastic:
  addresses: ["http://elasticsearch:9200"]
  timeout: 10s
  max_idle_conns_per_host: 32      # connection pool (Go default 2 is too low under load)
  # username/password | api_key    — auth
  # ca_cert_path / insecure_skip_verify — TLS (skip-verify: dev only)
  # max_retries: 3 | disable_retry — transport retries on 429/502/503/504
  # enable_metrics: true           — dx_elastic_* Prometheus metrics
```

## 2. Configuration reference

| Key | Default | Notes |
|-----|---------|-------|
| `addresses` | — required | node URLs |
| `username`/`password`/`api_key` | — | auth |
| `timeout` | `10s` | per-request bound |
| `max_idle_conns_per_host` | Go default (2) | HTTP keep-alive pool per node; set 32–100 for real traffic |
| `ca_cert_path` / `insecure_skip_verify` | — / false | TLS trust / dev-only skip |
| `max_retries` / `disable_retry` | 3 / false | retry on 429/502/503/504, exponential backoff |
| `enable_metrics` | false | `dx_elastic_requests_total{method,status}` + duration histogram |
| `Logger` (runtime) | — | zap: Debug per request, Warn on failure |
| `Transport` (runtime) | — | RoundTripper seam: test mocks + future OTel; overrides TLS fields |

## 3. Fluent search DSL

```go
items, total, err := dxelastic.SearchAs[domain.Item](ctx,
    es.NewSearch("catalogue").
        Filter(dxelastic.Term("status", "ACTIVE")).
        Filter(dxelastic.Term("provider.keyword", providerID)).
        Must(dxelastic.Match("description", keyword)).
        Highlight(dxelastic.Highlight{Fields: []string{"description"}}).
        SortDesc("createdAt").
        Page(page, size))
```

- **Builder verbs:** `Index(...)` via `NewSearch(indices...)` (multi-index), `Must/Should/MustNot/Filter`, `Query` (raw/function-score), `SortAsc/SortDesc`, `Page/From/Size/SearchAfter`, `Source/ExcludeSource`, `Agg/AggsOnly`, `Highlight`, `Suggest`, `KNN`, `PIT`, `TrackTotal`, then `Do` / `Count` / `SearchAs[T]`.
- **Query builders:** `MatchAll · Match · MatchPhrase · MatchFuzzy · MatchBoolPrefix (auto-complete) · MultiMatch · Term/Terms · Exists · Prefix · Wildcard · Regexp · Fuzzy · IDs · QueryString · Range · Nested · HasChild/HasParent/ParentID · GeoBoundingBox/GeoDistance/GeoShape · ScriptQuery · ScriptScore · FunctionScore(...).FieldValueFactor/Weight/Decay · Bool()`
- **Aggregations:** `TermsAgg · MetricAgg · DateHistogramAgg · FilterAgg · .Sub` (+ raw `Agg` maps for anything else).
- **Suggesters:** `TermSuggester` (spelling), `PhraseSuggester` (did-you-mean), `CompletionSuggester` (type-ahead over a `Completion` mapping field); results in `SearchResult.Suggest`.
- **Vector / hybrid:** map a field with `DenseVector(name, dims, similarity)`, search with `.KNN(KNN{Field, QueryVector, K})`; combine KNN with `Must/Filter` clauses and ES blends both — the hybrid form.
- Filter context (`Filter`) is cacheable and unscored — prefer it over `Must` for exact/term/range constraints.

## 4. Typed repository (`Repo[T]`)

`NewRepo[T](client, index)` — the `BaseSearchRepository`: `FindByID/Get · Exists · Index · Update · Delete · Count · Search · SearchAfter · BulkIndex · BulkDelete · ReindexTo · NewSearch()` (+ `Client()` escape hatch for aggregations/scripts/admin). Services add only domain-specific methods on top — see catalogue's `elasticrepo`.

Bulk semantics: `BulkDo(index, []BulkOp{IndexOp/UpdateOp/DeleteOp}, attempts)` returns `BulkStats` — per-item failures are **data**, not an error (a batch routinely partially succeeds); the whole batch retries only on transport failure. Keep bulk writes idempotent (stable ids).

## 5. Mapping framework

```go
body := dxelastic.NewMapping().
    Dynamic("strict").
    TextWithKeyword("name").
    Keyword("status").Date("createdAt").
    NestedField("attachments", dxelastic.NewMapping().Keyword("fileKey").Long("size")).
    DenseVector("embedding", 384, "cosine").
    CustomAnalyzer("en_text", "standard", "lowercase", "en_syn").
    Synonyms("en_syn", "tv => television").
    Shards(1, 1).Setting("refresh_interval", "30s").
    Build()                                   // → CreateIndex / EnsureIndex / MigrateIndex
```

- Field types: text (+keyword multi-field), keyword, date, long, double, boolean, geo_point/geo_shape, dense_vector, completion, object, **nested**, **join** (parent-child), plus raw `Field()` for anything else.
- Analysis: custom analyzers, tokenizers, token filters, **synonyms** (`synonym_graph`), normalizers — multi-language = one analyzer per language wired to per-language fields.
- `Dynamic("strict")` for production indices; `DynamicTemplate` + `RuntimeField` for controlled flexibility.
- **`AutoMap[T]()`** generates a mapping from a Go struct (json tags; `es:"keyword"` overrides; `es:"-"` skips; slices-of-struct → nested) — treat it as a *reviewed starting point*: generate, inspect, commit. `ValidateMapping` sanity-checks bodies in tests.
- **Versioning:** mappings are code, in the owning service, applied to versioned physical indices (`<alias>-vN`) behind a stable alias — same review/diff discipline as SQL migrations (DATABASE.md §8.4).

## 6. Index lifecycle (best practices)

| Concern | Practice | API |
|---------|----------|-----|
| Versioning | physical `name-vN`, clients only ever see the alias | `EnsureIndex`, `EnsureAlias` |
| Blue/green + zero-downtime reindex | create v(N+1) → copy → atomic alias swap → keep vN as instant rollback, delete later | **`MigrateIndex(alias, newIndex, body, opts)`** (orchestrates it; `DeleteOld:false` = rollback-safe) |
| Migrations | expand-only → `PutMapping` on the live index; anything else → `MigrateIndex` | `PutMapping` |
| Writes during copy | land in the old index — pause writes, dual-write, or run a catch-up `Reindex` after the swap | documented on `MigrateIndex` |
| Templates | pattern-matched settings/mappings for families of indices (logs/time-series) | `PutIndexTemplate` |
| Retention / ILM | hot→warm→delete policies for time-series; pair the policy with a template | `PutILMPolicy` |
| Snapshots / backup | ops-level: snapshot repository (S3/MinIO) + SLM schedule; restore = register repo + `_restore`. Not wrapped by this module — cluster admin, not service code | — |

## 7. Performance defaults (recommended)

| Area | Recommendation |
|------|----------------|
| Shards | 1 primary per index until >30–50 GB; never per-service shard sprawl on a small cluster |
| Replicas | 1 in prod (HA + read throughput); 0 in dev/single-node (cluster stays yellow otherwise — `HealthCheck` treats yellow as healthy for exactly this reason) |
| Refresh | leave `1s` default for interactive indices; `refresh_interval: 30s` for write-heavy; **bulk loads**: `UpdateIndexSettings` → `{refresh_interval: "-1", number_of_replicas: 0}`, load with `BulkDo` (1 000–5 000 docs or 5–15 MB per batch), restore settings, one `Refresh` |
| Never | per-document `Refresh`; giant `From` offsets; leading-wildcard/regex on hot paths |
| Pagination | `Page` (from/size) up to 10 000; past that **PIT + Sort + SearchAfter** (consistent snapshot, no server state per page); `TrackTotal` only when the exact count matters |
| Scroll vs search_after | Scroll only for full exports/ETL (then `ClearScroll`); user-facing deep pagination = PIT + search_after |
| Query shape | exact constraints in `Filter` (cached filter context); text relevance in `Must`; `Source(...)` to trim payloads; `AggsOnly` for facet-only calls |
| Connections | `max_idle_conns_per_host: 32+` under real traffic |
| Caching | ES's filter/request caches do the work when filters are stable; add app-level caching (dx-common-go `cache`) only for hot, slow aggregations |
| Large datasets | ILM tiers for time-series; `BulkStats`-driven ingest monitoring; dense_vector `num_candidates` ≈ 10×K (the default) as the recall/latency starting point |

## 8. Observability, health, errors

- Metrics + zap logging via `enable_metrics`/`Logger` — implemented as a `RoundTripper` wrapper; OTel tracing plugs into the same `Transport` seam later (ROADMAP PH-2).
- `HealthCheck` (green/yellow ok, red/unreachable fails) plugs into `dx-common-go/health`.
- All non-2xx responses map to `dxerrors` (`NotFound`/`Validation`/`Conflict`/`Internal`) — uniform handler translation.

## 9. Testing

- **Unit/mock, no ES**: point `NewClient` at an `httptest.Server` or inject `Config.Transport`. Mocks **must** send `X-Elastic-Product: Elasticsearch` (the official client's product check). Patterns: `client_test.go`, `framework_test.go`.
- **Integration**: gated on `ES_TEST_ADDR` (skips otherwise): `ES_TEST_ADDR=http://localhost:9200 go test ./database/elastic/...` against the dev stack.
