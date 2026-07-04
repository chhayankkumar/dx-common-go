# `database/elasticsearch` — the shared Elasticsearch framework

A production-grade, reusable Elasticsearch layer for every Go service. It owns **all**
Elasticsearch infrastructure — connection lifecycle, TLS/retries/observability, query building,
document CRUD, search, aggregations, highlighting, bulk, index mappings & lifecycle, deep
pagination, and a generic index-sync worker — so a service writes only its **domain** indexing
and search logic, never raw `go-elasticsearch` calls or hand-built query JSON.

> Restructured 2026-07-05 from the flat `database/elastic` package into cohesive sub-packages.
> The API is the same set of capabilities, re-homed; adopters import the sub-package they need.

---

## 1. Package layout

```
database/elasticsearch/
├── client/       transport: Client, Config, health; the single request seam (Do/DoNDJSON)
├── query/        pure DSL — build query.SearchRequest and query fragments; no I/O
├── repository/   execution + typed access: Repo[T], Search, document CRUD, scroll/PIT
├── mapping/      index schema (MappingBuilder, AutoMap) + lifecycle (alias/reindex/migrate/ILM)
└── indexing/     bulk engine + reindex + generic Source→bulk Syncer + supervised Worker
```

**Dependency direction (acyclic):**

```
query   (leaf)        client (leaf)
   \        /   \
    indexing      mapping
        \         /   \
         repository   (→ client, query)
```

`client` and `query` depend on nothing else in the tree. `indexing` → client, query. `mapping`
→ client, indexing. `repository` → client, query, indexing, mapping. A service composes whichever
layers it needs; the seams never cross back "up".

### What lives where

| Need | Package | Key API |
|------|---------|---------|
| Connect, TLS, retries, health, metrics | `client` | `client.New(Config)`, `*Client.HealthCheck`, `*Client.Do` |
| Build queries / requests | `query` | `query.Bool/Term/Range/Match/Geo*/FunctionScore`, `query.SearchRequest` |
| Typed repository over one index | `repository` | `repository.New[T](c, index)` → `Repo[T]` |
| Run a search, decode hits | `repository` | `repository.Search`, `repository.SearchAs[T]`, `HitsAs[T]` |
| Document CRUD, script/by-query updates | `repository` | `IndexDoc/GetDoc/UpdateDoc/DeleteDoc/Count/ScriptUpdate/UpdateByQuery` |
| Deep pagination / exports | `repository` | `Repo[T].SearchAfter`, `Scroll`, `OpenPIT` |
| Index mappings, analyzers, templates | `mapping` | `mapping.NewMapping()`, `mapping.AutoMap[T]()` |
| Create/ensure/alias/migrate indices | `mapping` | `EnsureIndex`, `EnsureAlias`, `SwapAlias`, `MigrateIndex`, `PutILMPolicy` |
| Bulk index/update/delete | `indexing` | `indexing.BulkDo`, `BulkIndexWithRetry` |
| Backfill / continuous sync from a source | `indexing` | `indexing.Sync`, `indexing.Worker` |

---

## 2. Adopting it in a service

```go
import (
    esclient "github.com/datakaveri/dx-common-go/database/elasticsearch/client"
    esquery  "github.com/datakaveri/dx-common-go/database/elasticsearch/query"
    esrepo   "github.com/datakaveri/dx-common-go/database/elasticsearch/repository"
)

// 1. Config (loadable from your service config tree via mapstructure)
type Config struct {
    Elastic esclient.Config `mapstructure:"elastic"`
}

// 2. Connect once at boot (pings the cluster — fails fast on misconfig)
c, err := esclient.New(cfg.Elastic)

// 3. A domain repository embeds the typed Repo[T] and adds ONLY domain methods
type ItemRepo struct{ *esrepo.Repo[Item] }
func NewItemRepo(c *esclient.Client) *ItemRepo {
    return &ItemRepo{esrepo.New[Item](c, "items")}
}

// domain method — infra comes from the embedded Repo[T]
func (r *ItemRepo) ActiveByOwner(ctx context.Context, owner string) ([]Item, int64, error) {
    return esrepo.SearchAs[Item](ctx, r.NewSearch().
        Filter(esquery.Term("status", "ACTIVE")).
        Filter(esquery.Term("owner", owner)).
        SortDesc("createdAt").
        Page(1, 20))
}
```

Health wiring:

```go
hh.Register("elasticsearch", health.NewCustomChecker("elasticsearch", c.HealthCheck))
```

---

## 3. Configuration reference (`client.Config`)

| Field | mapstructure | Purpose |
|-------|--------------|---------|
| `Addresses` | `addresses` | node URLs (required) |
| `Username` / `Password` / `APIKey` | `username`/`password`/`api_key` | auth |
| `Timeout` | `timeout` | per-request timeout (default 10s) |
| `CACertPath` | `ca_cert_path` | PEM bundle for private-CA TLS |
| `InsecureSkipVerify` | `insecure_skip_verify` | dev-only; skip TLS verify |
| `MaxRetries` / `DisableRetry` | `max_retries`/`disable_retry` | transport retry on 429/502/503/504 |
| `MaxIdleConnsPerHost` | `max_idle_conns_per_host` | keep-alive pool (set 32–100 under load) |
| `EnableMetrics` | `enable_metrics` | Prometheus `dx_elastic_*` metrics |
| `Logger` | (runtime) | zap request logging |
| `Transport` | (runtime) | inject an `http.RoundTripper` — test mocks / OTel |

---

## 4. Query DSL (`query`)

Compose queries structurally; values are always JSON-serialized (no string interpolation):

```go
q := esquery.Bool().
    Must(esquery.Match("description", "solar pump")).
    Filter(esquery.Term("status", "ACTIVE"), esquery.Range("createdAt").Gte("2026-01-01").Build()).
    MustNot(esquery.Exists("deletedAt")).
    Build()
```

Available: full-text (`Match`, `MatchPhrase`, `MultiMatch`, `MatchBoolPrefix`), term-level
(`Term`, `Terms`, `Exists`, `Prefix`, `Wildcard`, `Fuzzy`, `Regexp`, `IDs`, `QueryString`),
`Range`, geo (`GeoBoundingBox`, `GeoDistance`, `GeoShape`), parent-child (`HasChild`, `HasParent`,
`ParentID`, `Nested`), scoring (`FunctionScore`, `ScriptScore`), aggregations (`TermsAgg`,
`MetricAgg`, `DateHistogramAgg`, `FilterAgg`, `.Sub(...)`), `Highlight`, suggesters
(`TermSuggester`, `PhraseSuggester`, `CompletionSuggester`), and vector search (`KNN`).

Fluent search (`repository.NewSearch` / `Repo[T].NewSearch`) chains bool composition, sorting,
pagination, aggs, highlight, suggest, KNN and PIT, then terminates with `Do`/`Count`/`SearchAs[T]`.

---

## 5. Typed repository (`repository.Repo[T]`)

`repository.New[T](client, index)` gives `Get/FindByID/Exists/Index/Update/Delete/Count/Search/
SearchAfter/BulkIndex/BulkDelete/ReindexTo/NewSearch` — all decoding into `T`. `Client()` is the
documented escape hatch for anything the typed repo doesn't wrap (aggregations, scripts, admin).
Missing documents return a `dxerrors` NotFound; `client.IsNotFound(err)` turns that into a boolean.

---

## 6. Mapping & index lifecycle (`mapping`)

Mappings are **code** — reviewed, diffed, testable:

```go
body := mapping.NewMapping().
    Dynamic("strict").
    TextWithKeyword("name").
    Keyword("status").
    Date("createdAt").
    DenseVector("embedding", 384, "cosine").
    Shards(1, 1).
    Build()
mapping.EnsureIndex(ctx, c, "items-v1", body)
mapping.EnsureAlias(ctx, c, "items-v1", "items")   // address data via the stable alias
```

`mapping.AutoMap[T]()` derives a reviewed starting mapping from a struct. Evolve non-additively
with the blue/green rebuild: `mapping.MigrateIndex(ctx, c, alias, newIndex, body, opts)` creates
the new index, reindexes, and atomically swaps the alias. Also: `PutMapping` (additive),
`UpdateIndexSettings` (bulk-load tuning), `PutILMPolicy`, composable index templates.

---

## 7. Bulk, sync & workers (`indexing`)

```go
// one-shot bulk with partial-success stats + transport retry
stats, err := indexing.BulkDo(ctx, c, "items", ops, 3)

// generic backfill: drain any Source, bulk-index each batch
rep, err := indexing.Sync(ctx, c, myPostgresCursor, indexing.SyncConfig{Index: "items"})

// standing worker: re-sync on an interval, panic-safe, launch under errgroup
w := &indexing.Worker{Name: "items-sync", Interval: 5*time.Minute, RunOnStart: true,
    Job: func(ctx context.Context) error {
        _, err := indexing.Sync(ctx, c, newCursor(), indexing.SyncConfig{Index: "items"})
        return err
    }}
g.Go(func() error { return w.Start(ctx) })
```

`Source` is a one-method interface (`Next(ctx) (batch []Doc, done bool, err error)`) — implement
it over a Postgres cursor, a paged API, a file, a Kafka partition. For sub-minute cadence or
cross-replica singleton coordination, register the job with `dx-common-go/scheduler` instead.

---

## 8. Observability, health, errors

- **Metrics** (`EnableMetrics: true`): `dx_elastic_requests_total{method,status}` +
  `dx_elastic_request_duration_seconds{method}` on the default Prometheus registry.
- **Logging** (`Logger`): each request at Debug, failures at Warn.
- **Tracing**: `Config.Transport` is the seam — wrap with `otelhttp.NewTransport` when OTel lands.
- **Health**: `*Client.HealthCheck` (nil for green/yellow, error for red/unreachable).
- **Errors**: every non-2xx maps to the shared `dxerrors` taxonomy (404→NotFound, 400→Validation,
  409→Conflict, else Internal) — handlers get the right HTTP status for free.

---

## 9. Testing

- **Unit**: inject `Config.Transport` (an `http.RoundTripper`) for canned responses with no server,
  or point `Addresses` at an `httptest.Server` (set the `X-Elastic-Product: Elasticsearch` header —
  the official client requires it). See each package's `*_test.go`.
- **Integration**: set `ES_TEST_ADDR` to a real cluster; `repository/integration_test.go` runs
  against it (skips otherwise, so plain `go test ./...` never needs a cluster).

---

## 10. Extending the framework (for future needs)

- **New query type** → add a constructor to `query` (pure, no I/O). It's just a `query.Query` map.
- **New ES operation** → a package-level function `func Op(ctx, c *client.Client, …)` in the layer
  it belongs to (`repository` for reads/CRUD, `mapping` for schema/lifecycle, `indexing` for writes),
  built on `client.Do` / `client.DoNDJSON`. Never add a new low-level HTTP client.
- **Service-specific logic stays in the service** — field boosts, domain filters, business rules.
  If you find yourself writing >~30 lines of generic ES infra in a service, it belongs here (open a
  PR to `dx-common-go`).
- **Rule for new repos**: embed `*repository.Repo[T]` and write only domain methods.

### How future services adopt it (minimal effort)
1. Add `esclient.Config` to your config tree; set `addresses` (+ auth/TLS as needed).
2. `c := esclient.New(cfg.Elastic)` at boot; register `c.HealthCheck`.
3. Define your document type `T`; `esrepo.New[T](c, index)` (embed it in a domain repo).
4. Build searches with the `query` DSL / fluent `NewSearch`; provision indices with `mapping`.
5. Reach for `indexing.Sync`/`Worker` only if you backfill or continuously index from a source.
