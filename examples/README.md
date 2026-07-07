# dx-common-go examples

A **separate Go module** (its own `go.mod`, `replace`-ing the parent) so demo-only
dependencies never enter the library's dependency graph. CI builds this module —
that build is what keeps the reference wiring from rotting as the framework evolves.

```bash
cd examples
go build ./...
```

## `minimal-service`

The canonical service wiring, top to bottom, in one `main.go`. It is deliberately
tiny (one table, one endpoint) so it reads as a template you copy-and-rename, not a
service you study. It exercises the whole framework path in order:

| Step | Package | What it shows |
|------|---------|---------------|
| Config | `config.LoadService[Config]` | file + env + defaults; reuse `dxclient.Config`, don't redeclare |
| Observability | `observability.Init` | OTel SDK lifecycle; no-op without an endpoint |
| Migrations | `database/postgres/migrate` | embedded `NNNN_*.up/.down.sql`, per-service history table |
| Pool + tracers | `database/postgres/client` | `NewPool(WithTracers(otelpgx, SlowQueryTracer))` — covers DSL/sqlc/raw |
| Repository | `database/postgres/repository` | embed `Base[R]`, options-based `New`, domain methods only |
| Outbox + scheduler | `messaging/outbox`, `scheduler` | durable at-least-once events; `WithSingleton` advisory lock |
| Middleware | `middleware.Standard(WithTracing())` | RequestID, Logger, CORS, tracing, timeout, recover |
| Health | `health` | `/healthz/live` + `/healthz/ready` with a pgx checker |
| Serve | `httpserver` | graceful drain on SIGINT/SIGTERM |

### Run it

Needs a reachable Postgres (the default DSN targets `localhost:5432`).

```bash
cd examples/minimal-service
DX_POSTGRES_DSN="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" \
  go run .
# GET http://localhost:8080/widgets
# GET http://localhost:8080/healthz/ready
```

Every config key has a `DX_`-prefixed env override (`DX_SERVER_PORT`,
`DX_SCHEMA_MODE`, `DX_OTEL_ENDPOINT`, `DX_POSTGRES_DSN`). Point `DX_OTEL_ENDPOINT`
at an OTLP/gRPC collector to see spans across HTTP → Postgres.
