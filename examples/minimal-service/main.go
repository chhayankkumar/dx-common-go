// Command minimal-service is the reference wiring for a CDPG Go service on
// dx-common-go. It is intentionally tiny — one table, one endpoint — but it
// exercises the whole framework path in the canonical order, so a new service
// can be bootstrapped by copy-and-rename:
//
//	config → observability → migrations → pool(+tracers) → repository →
//	outbox + scheduler → middleware.Standard(WithTracing) → health → serve
//
// It lives in a separate Go module (examples/) so its demo-only dependencies
// stay out of the library, and it is compiled in CI so the template can't rot.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	dxconfig "github.com/datakaveri/dx-common-go/config"
	dxclient "github.com/datakaveri/dx-common-go/database/postgres/client"
	dxmigrate "github.com/datakaveri/dx-common-go/database/postgres/migrate"
	"github.com/datakaveri/dx-common-go/database/postgres/query"
	"github.com/datakaveri/dx-common-go/database/postgres/repository"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
	"github.com/datakaveri/dx-common-go/health"
	"github.com/datakaveri/dx-common-go/httpserver"
	"github.com/datakaveri/dx-common-go/messaging/outbox"
	dxmw "github.com/datakaveri/dx-common-go/middleware"
	"github.com/datakaveri/dx-common-go/observability"
	dxresp "github.com/datakaveri/dx-common-go/response"
	"github.com/datakaveri/dx-common-go/scheduler"
)

// Config is loaded once at boot. Every field comes from the shared config
// loader — reuse dxclient.Config for Postgres rather than redeclaring it.
type Config struct {
	ServerPort   string          `mapstructure:"server_port"`
	SchemaMode   string          `mapstructure:"schema_mode"`
	OTelEndpoint string          `mapstructure:"otel_endpoint"`
	Postgres     dxclient.Config `mapstructure:"postgres"`
}

// widget is the demo domain row. The `db` tags drive pgx struct scanning.
type widget struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// widgetRepo is the whole repository: embed repository.Base and add only the
// domain methods you need. Every inherited CRUD/query/paging call is already
// transaction-propagation-aware.
type widgetRepo struct {
	*repository.Base[widget]
}

func newWidgetRepo(pool *pgxpool.Pool) *widgetRepo {
	return &widgetRepo{
		repository.New[widget](pool,
			repository.WithTable[widget]("widgets"),
			repository.WithID[widget]("id"),
		),
	}
}

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck

	cfg, err := dxconfig.LoadService[Config](dxconfig.ServiceOptions{
		EnvPrefix: "DX",
		Defaults: map[string]any{
			"server_port":  "8080",
			"schema_mode":  dxmigrate.ModeMigrate,
			"postgres.dsn": "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable",
		},
	})
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Observability — wires the OTel SDK, or no-ops when no endpoint is
	//    configured. Every downstream tracer (pool, middleware) reads from it.
	shutdown, err := observability.Init(ctx, observability.Config{
		ServiceName: "minimal-service",
		Endpoint:    cfg.OTelEndpoint,
	})
	if err != nil {
		logger.Fatal("init observability", zap.Error(err))
	}
	defer shutdown(context.Background()) //nolint:errcheck

	// 2. Migrations — this service owns widgets + widget_outbox, tracked in
	//    its own history table (shared-DB convention).
	if err := dxmigrate.Run(dxmigrate.Config{
		Mode:      cfg.SchemaMode,
		DSN:       cfg.Postgres.DSN,
		TableName: "schema_migrations_minimal",
	}, migrations, "migrations", logger); err != nil {
		logger.Fatal("run migrations", zap.Error(err))
	}

	// 3. Pool with tracers — OTel spans + a 200ms slow-query log, composed onto
	//    pgx's single Tracer slot so DSL, sqlc, and raw paths are all covered.
	pool, err := dxclient.NewPool(cfg.Postgres, dxclient.WithTracers(
		otelpgx.NewTracer(),
		&dxclient.SlowQueryTracer{Threshold: 200 * time.Millisecond, Logger: logger},
	))
	if err != nil {
		logger.Fatal("connect postgres", zap.Error(err))
	}
	defer pool.Close()

	// 4. Repository — domain methods only; CRUD/query come from Base.
	repo := newWidgetRepo(pool)

	// 5. Transactional outbox + in-process scheduler — durable at-least-once
	//    event publishing without a distributed transaction. WithSingleton
	//    means only one replica drains at a time (advisory try-lock).
	store := outbox.NewPGStore(pool, "widget_outbox")
	dispatcher := outbox.NewDispatcher(store, demoPublish(logger), logger, outbox.WithInterval(5*time.Second))
	sched := scheduler.New(logger)
	sched.Register(dispatcher.Job("minimal-widget-outbox"), scheduler.WithSingleton(pool))
	go func() {
		// Start blocks until ctx is cancelled (SIGINT/SIGTERM), then drains.
		if err := sched.Start(ctx); err != nil {
			logger.Error("scheduler stopped", zap.Error(err))
		}
	}()

	// 6. HTTP — the standard middleware stack with tracing enabled, health
	//    probes, and one demo handler. The service writer tags responses with
	//    this service's URN prefix.
	writer := dxresp.NewServiceWriter("urn:dx:minimal:")
	r := chi.NewRouter()
	dxmw.Standard(logger, 30*time.Second, dxmw.WithTracing())(r)

	hh := health.NewHandler()
	hh.Register("database", health.NewPgxPoolChecker(pool))
	r.Get("/healthz/live", hh.Live)
	r.Get("/healthz/ready", hh.Ready)

	r.Get("/widgets", listWidgets(repo, writer, logger))

	// 7. Serve — blocks until SIGINT/SIGTERM, then drains in-flight requests;
	//    the same signal cancels ctx, stopping the scheduler.
	srvCfg := httpserver.DefaultConfig()
	if p, perr := strconv.Atoi(cfg.ServerPort); perr == nil {
		srvCfg.Port = p
	}
	logger.Info("minimal-service starting", zap.String("port", cfg.ServerPort))
	if err := httpserver.New(srvCfg, r, logger).Start(); err != nil {
		logger.Fatal("server error", zap.Error(err))
	}
}

// listWidgets returns the newest widgets first — a one-liner over the embedded
// Base, with the shared response envelope and error taxonomy.
func listWidgets(repo *widgetRepo, w *dxresp.ServiceWriter, logger *zap.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		items, err := repo.FindAllOrdered(r.Context(), nil, []query.OrderBy{{Column: "created_at", Desc: true}})
		if err != nil {
			dxerrors.WriteServerError(rw, err, func(e error) {
				logger.Error("list widgets", zap.Error(e))
			})
			return
		}
		w.Success(rw, items, "widgets", "listed widgets")
	}
}

// demoPublish stands in for a real broker publish — the outbox.Dispatcher's
// callback. A production service returns rabbitmq.ReliablePublisher.Publish
// here (with Confirms enabled, so MarkSent only fires once the broker has it).
func demoPublish(logger *zap.Logger) outbox.Publish {
	return func(ctx context.Context, row outbox.Row) error {
		logger.Info("outbox row published (demo — no real broker)",
			zap.String("action", row.Action), zap.Int("attempts", row.Attempts))
		return nil
	}
}
