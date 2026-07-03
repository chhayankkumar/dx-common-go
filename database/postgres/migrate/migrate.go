package migrate

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	pgxdriver "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"go.uber.org/zap"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver used below
)

// Mode values for Config.Mode.
const (
	// ModeNone is a no-op. Use it while a service's tables are still
	// Flyway-owned (the legacy-baseline interim state) — see doc.go.
	ModeNone = "none"
	// ModeMigrate runs every pending migration up to the latest version.
	ModeMigrate = "migrate"
)

// Config configures Run and Status. Mode is a plain string (compare against
// ModeNone/ModeMigrate) rather than a distinct named type, so it binds
// directly from a service's own config field (e.g. `SchemaMode string`)
// without a conversion at the call site.
type Config struct {
	// Mode is ModeNone or ModeMigrate.
	Mode string
	// DSN is the Postgres connection string.
	DSN string
	// TableName is this service's own migrations-history table — the
	// x-migrations-table convention (schema_migrations_<service>) so
	// multiple services can share one interim database without colliding
	// version tracking. See doc.go.
	TableName string
}

// DirtyStateError reports that the migrations table is marked dirty: a
// previous migration failed partway through and golang-migrate refuses to
// run anything further until it's resolved. Callers must treat this as
// fatal — never start the service against a dirty schema.
type DirtyStateError struct {
	Version uint
	Table   string
}

func (e *DirtyStateError) Error() string {
	return fmt.Sprintf(
		"migrate: migrations table %q is dirty at version %d — a prior migration failed partway "+
			"through; fix the database by hand, then `migrate force %d` before restarting",
		e.Table, e.Version, e.Version,
	)
}

// Run applies every pending migration under dir (an embedded directory of
// NNNN_title.up.sql / NNNN_title.down.sql pairs, see doc.go) to cfg.DSN,
// tracked in cfg.TableName. cfg.Mode == ModeNone is a no-op, so callers can
// invoke Run unconditionally from cmd/server/main.go and gate real
// execution purely through config. A nil logger is fine (logging is skipped).
func Run(cfg Config, fsys fs.FS, dir string, logger *zap.Logger) error {
	if cfg.Mode != ModeMigrate {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	m, closeFn, err := open(cfg, fsys, dir)
	if err != nil {
		return err
	}
	defer closeFn()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.Info("no pending migrations", zap.String("table", cfg.TableName))
			return nil
		}
		var dirty migrate.ErrDirty
		if errors.As(err, &dirty) {
			return &DirtyStateError{Version: uint(dirty.Version), Table: cfg.TableName}
		}
		return fmt.Errorf("migrate: up: %w", err)
	}

	version, _, verr := m.Version()
	if verr == nil {
		logger.Info("migrations applied", zap.String("table", cfg.TableName), zap.Uint("version", version))
	}
	return nil
}

// Status reports the current schema version and dirty flag without applying
// anything, for a boot-time "refuse to start if the DB is ahead of this
// binary" check. version=0, dirty=false, err=nil means no migration has run yet.
func Status(cfg Config, fsys fs.FS, dir string) (version uint, dirty bool, err error) {
	m, closeFn, err := open(cfg, fsys, dir)
	if err != nil {
		return 0, false, err
	}
	defer closeFn()

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("migrate: version: %w", err)
	}
	return version, dirty, nil
}

func open(cfg Config, fsys fs.FS, dir string) (*migrate.Migrate, func(), error) {
	src, err := iofs.New(fsys, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: open source %q: %w", dir, err)
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: open db: %w", err)
	}

	dbDriver, err := pgxdriver.WithInstance(db, &pgxdriver.Config{MigrationsTable: cfg.TableName})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate: init driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx", dbDriver)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate: new instance: %w", err)
	}

	// dbDriver.Close (invoked via m.Close) closes db itself, so there is
	// nothing left for the caller to close separately.
	closeFn := func() { _, _ = m.Close() }
	return m, closeFn, nil
}
