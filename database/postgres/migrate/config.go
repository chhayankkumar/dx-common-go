package migrate

import "fmt"

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
