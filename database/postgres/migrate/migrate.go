// Package migrate runs versioned SQL migrations embedded in a service binary,
// implementing the platform migration policy (DATABASE.md §7.2, plan Q7/W8):
//
//   - Tooling: golang-migrate, migrations embedded via embed.FS (iofs source)
//     as paired NNNN_title.up.sql / NNNN_title.down.sql files, zero-padded
//     sequential versions.
//   - Mode gate: "none" while a service points at the shared legacy iudx_db
//     and owns no tables there; "migrate" when it owns net-new tables on the
//     shared DB or its own database. Legacy tables remain Flyway-owned — a
//     service's migrations may only ever touch tables it owns.
//   - Per-service version table: on the shared iudx_db every service MUST set
//     TableName to its own history table (e.g. "schema_migrations_acl") so
//     services never share migration state. On a dedicated DB the default
//     "schema_migrations" is fine.
//   - Failure semantics: golang-migrate marks the database dirty when a
//     migration fails mid-way and refuses further runs until resolved. Run
//     reports this loudly with the exact failed version and the recovery
//     procedure, because the operator otherwise sees only a cryptic
//     "Dirty database version N" error.
//
// Migrations run before the service opens its normal pool — call Run from
// main() (or an init job) before NewPool.
package migrate

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	gomigrate "github.com/golang-migrate/migrate/v4"
	pgxmig "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
	"go.uber.org/zap"
)

// Modes for Config.Mode.
const (
	// ModeNone runs no DDL at all — for services on the shared legacy DB that
	// own no tables (Flyway owns everything they touch).
	ModeNone = "none"
	// ModeMigrate applies pending versioned migrations (the default).
	ModeMigrate = "migrate"
)

// DefaultTable is the golang-migrate default history table, used when a
// service owns its whole database.
const DefaultTable = "schema_migrations"

// Config wires one service's migration run.
type Config struct {
	// Mode is "migrate" (default) or "none".
	Mode string `mapstructure:"mode"`
	// DSN is the Postgres connection string (same one the service pool uses).
	DSN string `mapstructure:"-"`
	// TableName is the migration-history table. MUST be service-scoped
	// (e.g. "schema_migrations_acl") while several services share one
	// database; defaults to DefaultTable.
	TableName string `mapstructure:"table_name"`
}

// Run applies pending migrations from the given fs (typically an embed.FS)
// under dir (typically "migrations"). It is safe to call on every boot: an
// up-to-date database is a no-op.
func Run(cfg Config, migrations fs.FS, dir string, logger *zap.Logger) error {
	switch cfg.Mode {
	case ModeNone:
		logger.Info("schema migrations disabled (mode=none) — schema owned externally")
		return nil
	case "", ModeMigrate:
		// proceed
	default:
		return fmt.Errorf("migrate: unknown mode %q (want %q or %q)", cfg.Mode, ModeMigrate, ModeNone)
	}
	if cfg.DSN == "" {
		return errors.New("migrate: DSN is required")
	}

	src, err := iofs.New(migrations, dir)
	if err != nil {
		return fmt.Errorf("migrate: load embedded migrations: %w", err)
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return fmt.Errorf("migrate: open database: %w", err)
	}
	// db is owned by the migrate instance from here; m.Close closes it.

	table := cfg.TableName
	if table == "" {
		table = DefaultTable
	}
	drv, err := pgxmig.WithInstance(db, &pgxmig.Config{MigrationsTable: table})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("migrate: init pgx driver: %w", err)
	}

	m, err := gomigrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("migrate: init runner: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil || dbErr != nil {
			logger.Warn("migrate: close", zap.NamedError("source", srcErr), zap.NamedError("db", dbErr))
		}
	}()

	err = m.Up()
	switch {
	case err == nil:
		v, _, _ := m.Version()
		logger.Info("schema migrations applied", zap.Uint("version", v), zap.String("table", table))
		return nil
	case errors.Is(err, gomigrate.ErrNoChange):
		v, _, _ := m.Version()
		logger.Info("schema up to date", zap.Uint("version", v), zap.String("table", table))
		return nil
	default:
		// Loud dirty-state reporting: without this the operator only sees
		// "Dirty database version N" with no recovery path.
		if v, dirty, verr := m.Version(); verr == nil && dirty {
			logger.Error("MIGRATION FAILED MID-RUN — database is marked dirty and no further "+
				"migrations will run until resolved. Recovery: inspect and manually fix/undo the "+
				"partial changes of the failed version, then clear the dirty flag with "+
				"`migrate -path <dir> -database <dsn>?x-migrations-table="+table+" force <previous-version>` "+
				"(or delete the row from the history table), and redeploy.",
				zap.Uint("failed_version", v), zap.String("table", table), zap.Error(err))
		}
		return fmt.Errorf("migrate: %w", err)
	}
}
