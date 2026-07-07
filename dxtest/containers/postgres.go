// Package containers gives tests a real Postgres/Redis to run against, with
// a DSN escape hatch for environments where Docker isn't available (or
// isn't wanted) — set DX_TEST_PG_DSN / DX_TEST_REDIS_ADDR to bind to an
// external instance instead of starting a container. Same Go module as the
// rest of dx-common-go (a second module would break the replace-directive
// simplicity every service relies on); only test binaries actually import
// this package, so the extra dependency weight (Docker client, etc.) never
// reaches a service's production binary.
package containers

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	pgtc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"go.uber.org/zap"

	dxmigrate "github.com/datakaveri/dx-common-go/database/postgres/migrate"

	dxclient "github.com/datakaveri/dx-common-go/database/postgres/client"
)

// PostgresHandle is a ready-to-use Postgres connection for a test.
type PostgresHandle struct {
	Pool *pgxpool.Pool
	DSN  string
}

// Option configures Postgres.
type Option func(*postgresConfig)

type postgresConfig struct {
	migrations    fs.FS
	migrationsDir string
	setupSQL      fs.FS
	setupSQLDir   string
}

// WithMigrations applies the versioned migrations in fsys (rooted at dir)
// via database/postgres/migrate.Run before returning the handle — the same
// wrapper a service's main() uses at boot, so a test exercises the exact
// migration path production does.
func WithMigrations(fsys fs.FS, dir string) Option {
	return func(c *postgresConfig) { c.migrations, c.migrationsDir = fsys, dir }
}

// WithSetupSQL runs every *.sql file under dir (in fsys, sorted by name)
// directly against the connected pool, once per Postgres(t, ...) call — for
// seeding a library's own test fixture schema, as opposed to WithMigrations,
// which exercises a service's real versioned migration path. Because this
// runs on every call (not just the container's first boot, unlike a native
// testcontainers init script), the SQL must be idempotent — CREATE TABLE IF
// NOT EXISTS, no seed INSERTs — since the underlying container (and
// whatever an earlier call already created) is shared across every
// Postgres(t, ...) call in one test binary.
func WithSetupSQL(fsys fs.FS, dir string) Option {
	return func(c *postgresConfig) { c.setupSQL, c.setupSQLDir = fsys, dir }
}

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
)

// Postgres returns a PostgresHandle bound to a real Postgres instance.
//
// If DX_TEST_PG_DSN is set, it binds to that external instance (today's
// Docker-less CI path — preserved, never removed). Otherwise it starts one
// testcontainers-go Postgres container, shared across every Postgres(t, ...)
// call in this test binary (started once via sync.Once) — cheaper than one
// container per test, and left running for testcontainers-go's own reaper
// (ryuk) to tear down when the binary exits, rather than each test racing to
// terminate a container others may still be using.
func Postgres(t *testing.T, opts ...Option) *PostgresHandle {
	t.Helper()
	cfg := &postgresConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	dsn := os.Getenv("DX_TEST_PG_DSN")
	if dsn == "" {
		dsn = startPostgresContainer(t)
	}

	if cfg.migrations != nil {
		if err := dxmigrate.Run(dxmigrate.Config{
			Mode:      dxmigrate.ModeMigrate,
			DSN:       dsn,
			TableName: "schema_migrations_dxtest",
		}, cfg.migrations, cfg.migrationsDir, zap.NewNop()); err != nil {
			t.Fatalf("dxtest/containers: apply migrations: %v", err)
		}
	}

	pool, err := dxclient.NewPool(dxclient.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("dxtest/containers: connect pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if cfg.setupSQL != nil {
		applySetupSQL(t, pool, cfg.setupSQL, cfg.setupSQLDir)
	}

	return &PostgresHandle{Pool: pool, DSN: dsn}
}

func applySetupSQL(t *testing.T, pool *pgxpool.Pool, fsys fs.FS, dir string) {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("dxtest/containers: read setup SQL dir %q: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	ctx := context.Background()
	for _, name := range names {
		contents, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			t.Fatalf("dxtest/containers: read setup SQL file %q: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("dxtest/containers: apply setup SQL %q: %v", name, err)
		}
	}
}

func startPostgresContainer(t *testing.T) string {
	t.Helper()
	// Skip (not fail) every test that reaches here when Docker itself isn't
	// available — a missing/unreachable Docker daemon is an environment
	// capability gap, not a test failure, and every caller must check this
	// for itself even though the container start below only happens once.
	testcontainers.SkipIfProviderIsNotHealthy(t)

	pgOnce.Do(func() {
		ctx := context.Background()
		container, err := pgtc.Run(ctx, "postgres:16-alpine",
			pgtc.WithDatabase("dxtest"),
			pgtc.WithUsername("dxtest"),
			pgtc.WithPassword("dxtest"),
			pgtc.BasicWaitStrategies(),
		)
		if err != nil {
			pgErr = fmt.Errorf("start postgres container: %w", err)
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = fmt.Errorf("postgres container connection string: %w", err)
			return
		}
		pgDSN = dsn
	})
	if pgErr != nil {
		t.Fatalf("dxtest/containers: %v", pgErr)
	}
	return pgDSN
}
