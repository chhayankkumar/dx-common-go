# Data Access in dx-common-go

Shared data-access layer for all CDPG Go services, mirroring the Java
dx-common framework (`AbstractBaseDAO`, query models, `Condition` composite,
`ElasticsearchService`). Use these packages instead of hand-writing SQL/DSL:
you inherit parameterized query building, struct scanning, pagination, and
Postgres/Elasticsearch error translation to `dxerrors`.

## PostgreSQL

### Defining a DAO for a new table (the 3-line pattern)

```go
import (
    "github.com/datakaveri/dx-common-go/database/postgres/dao"
)

// Model fields map to columns via `db` tags (pgx RowToStructByNameLax).
type Organization struct {
    ID        string    `db:"org_id"`
    Name      string    `db:"org_name"`
    Sector    string    `db:"org_sector"`
    CreatedAt time.Time `db:"created_at"`
}

orgDAO := dao.NewBaseDAO[Organization](pool, "organizations")
orgDAO.IDColumn = "org_id" // default is "id"
```

That's the whole DAO. Inherited operations:

```go
org, err  := orgDAO.FindByID(ctx, id)
orgs, err := orgDAO.FindAll(ctx, conds)
one, err  := orgDAO.FindOne(ctx, conds)
page, err := orgDAO.FindPage(ctx, conds, orderBy, limit, offset) // Page{Data, Total, HasNext}
n, err    := orgDAO.Count(ctx, conds)
org, err  := orgDAO.InsertMap(ctx, map[string]any{"org_name": "ACME"}) // RETURNING *
org, err  := orgDAO.UpdateReturning(ctx, set, conds)
org, err  := orgDAO.Upsert(ctx, fields, "org_id", []string{"org_name"})
err       := orgDAO.SoftDelete(ctx, id)   // status='DELETED'
err       := orgDAO.HardDelete(ctx, conds)
```

All errors are translated by `dao.MapPgError`: `pgx.ErrNoRows` → `dxerrors.NotFound`,
unique violation → `Conflict`, FK/NOT NULL/CHECK → `Validation`, deadlock → `Database`.
Handlers can pass them straight to `dxerrors.WriteError`.

### Conditions & filters

```go
import "github.com/datakaveri/dx-common-go/database/postgres/query"

conds := query.NewConditionBuilder().
    Eq("status", "ACTIVE").
    In("category", []string{"A", "B"}).        // renders col = ANY($n)
    Between("created_at", from, to).
    Like("s3_key", prefix+"%").
    Build()

// Request-level filter maps (skips nil/"" values, slice → ANY):
conds := query.FromFilters(map[string]any{
    "subject_id":   req.SubjectID,
    "resource_ids": req.ResourceIDs,
})

// Temporal filters (Java TemporalRequest equivalent):
conds = append(conds, query.FromTemporal([]query.TemporalFilter{
    {Field: "created_at", Rel: "between", Time: from, End: to},
})...)
```

### Transactions

Every DAO runs on a `Querier` (satisfied by `*pgxpool.Pool` and `pgx.Tx`):

```go
err := postgres.WithTransaction(ctx, pool, func(tx pgx.Tx) error {
    txDAO := orgDAO.WithTx(tx)
    if _, err := txDAO.InsertMap(ctx, fields); err != nil {
        return err
    }
    return outboxDAO.WithTx(tx).Insert(ctx, cols, vals) // same tx
})
```

### Raw-SQL escape hatch (CTEs, window functions, jsonb)

Keep complex SQL, but share scanning + error translation:

```go
rows, err := orgDAO.Select(ctx, `
    WITH counts AS (SELECT org_id, COUNT(*) c FROM members GROUP BY org_id)
    SELECT o.*, COALESCE(c.c, 0) AS member_count
      FROM organizations o LEFT JOIN counts c USING (org_id)
     WHERE o.status = $1`, "ACTIVE")
one, err := orgDAO.SelectOne(ctx, sql, args...)
n, err   := orgDAO.Exec(ctx, sql, args...)
```

### Standalone query builder

When you only want SQL strings (no DAO), `query.New().BuildSelect/Insert/
Update/Delete/Upsert` return `(sql, args)` for direct pgx use — see
dx-acl-go `policy_repo.go List()` for a working example.

## Elasticsearch

```go
import "github.com/datakaveri/dx-common-go/database/elastic"

es, err := elastic.NewClient(elastic.Config{
    Addresses: []string{"http://elasticsearch:9200"},
    Username:  "elastic", Password: "…",
})

res, err := es.Search(ctx, "iudx-docs", elastic.SearchRequest{
    Query: elastic.Bool().
        Must(elastic.MultiMatch("solar pump", "title^3", "description")).
        Filter(elastic.Term("status", "ACTIVE"),
               elastic.Range("created_at").Gte("2026-01-01").Build()).
        Build(),
    Size: 20, From: 0,
    Sort: []map[string]string{{"created_at": "desc"}},
    Aggregations: map[string]elastic.Agg{
        "by_category": elastic.TermsAgg("category", 10),
    },
})

docs, err := elastic.HitsAs[MyDoc](res)   // typed hits
total := res.Total
raw := res.Aggregations["by_category"]    // json.RawMessage
```

Other operations: `Count`, `IndexDoc`, `GetDoc`, `UpdateDoc` (partial),
`DeleteDoc`, `DeleteByQuery`, `BulkIndex`, `CreateIndex`, `IndexExists`.
HTTP 404/400/409 map to `dxerrors` NotFound/Validation/Conflict.

## S3 / MinIO

`storage/s3` implements `StorageRepository`: Put/Get/Delete/List, presigned
GET/PUT URLs, full multipart-upload lifecycle, plus `ObjectExists` and
`CopyObject`. Construct once per service with `s3.NewClient(cfg)`.

## Redis & cache

`database/redis` is the typed client (`SetJSON`/`GetJSON`/`Increment`/`TTL`);
`cache` adds the `Cache` interface (Redis or in-memory) and `CacheHelper`
with `GetJSONOrFetch` for read-through caching.

## Migration guidance

- New repositories: start from `BaseDAO[T]` + `db` tags (see
  dx-files-connect-api-go `internal/repository/file_repository.go`).
- Filtered list endpoints: `query.FromFilters` + `FindPage` (see dx-acl-go
  `policy_repo.go`).
- CTE-heavy queries (community-layer): keep raw SQL but run it through
  `dao.Select/SelectOne/Exec` for shared scanning and error mapping.
