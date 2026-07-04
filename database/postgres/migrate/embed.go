package migrate

import (
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	pgxdriver "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver used below
)

// open wires an embedded migration directory (fsys/dir, via golang-migrate's
// iofs source) to cfg.DSN through the pgx database/sql driver, ready for
// Run/Status to call Up/Version on.
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
