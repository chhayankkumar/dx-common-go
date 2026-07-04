package migrate_test

import (
	"context"
	"embed"
	"errors"
	"strings"
	"testing"

	dxmigrate "github.com/datakaveri/dx-common-go/database/postgres/migrate"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
)

// A separate external test package (migrate_test, not migrate) is required
// here — dxtest/containers imports database/postgres/migrate itself, so an
// internal test file (package migrate) importing dxtest/containers would be
// a real import cycle.
//
//go:embed testdata
var testFS embed.FS

func tableName(t *testing.T) string {
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return strings.ToLower("schema_migrations_" + name)
}

func TestRun_AppliesAndIsIdempotent(t *testing.T) {
	h := containers.Postgres(t)
	table := tableName(t)
	cfg := dxmigrate.Config{Mode: dxmigrate.ModeMigrate, DSN: h.DSN, TableName: table}
	t.Cleanup(func() { h.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	if err := dxmigrate.Run(cfg, testFS, "testdata", nil); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	// Second call must hit golang-migrate's ErrNoChange path internally and
	// still return nil — proving Run's idempotent-rerun handling works
	// against a real Postgres, not just in the ModeNone unit test.
	if err := dxmigrate.Run(cfg, testFS, "testdata", nil); err != nil {
		t.Fatalf("second (idempotent) Run: %v", err)
	}
}

func TestStatus_ReportsVersionAndDirty(t *testing.T) {
	h := containers.Postgres(t)
	table := tableName(t)
	cfg := dxmigrate.Config{Mode: dxmigrate.ModeMigrate, DSN: h.DSN, TableName: table}
	t.Cleanup(func() { h.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	if err := dxmigrate.Run(cfg, testFS, "testdata", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	version, dirty, err := dxmigrate.Status(cfg, testFS, "testdata")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if version != 1 || dirty {
		t.Fatalf("expected version=1 dirty=false, got version=%d dirty=%v", version, dirty)
	}
}

func TestStatus_NoMigrationsRunYet_ReturnsZero(t *testing.T) {
	h := containers.Postgres(t)
	table := tableName(t)
	cfg := dxmigrate.Config{Mode: dxmigrate.ModeMigrate, DSN: h.DSN, TableName: table}
	t.Cleanup(func() { h.Pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	version, dirty, err := dxmigrate.Status(cfg, testFS, "testdata")
	if err != nil {
		t.Fatalf("Status before any Run: %v", err)
	}
	if version != 0 || dirty {
		t.Fatalf("expected version=0 dirty=false before any migration ran, got version=%d dirty=%v", version, dirty)
	}
}

func TestRun_DirtyState_ReturnsDirtyStateError(t *testing.T) {
	h := containers.Postgres(t)
	table := tableName(t)
	cfg := dxmigrate.Config{Mode: dxmigrate.ModeMigrate, DSN: h.DSN, TableName: table}
	ctx := context.Background()
	t.Cleanup(func() { h.Pool.Exec(ctx, "DROP TABLE IF EXISTS "+table) })

	if err := dxmigrate.Run(cfg, testFS, "testdata", nil); err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	// Manually mark the migrations table dirty, simulating a prior migration
	// that failed partway through — golang-migrate refuses to run anything
	// further until this is cleared by hand.
	if _, err := h.Pool.Exec(ctx, "UPDATE "+table+" SET dirty = true"); err != nil {
		t.Fatalf("force dirty state: %v", err)
	}

	err := dxmigrate.Run(cfg, testFS, "testdata", nil)
	var dirtyErr *dxmigrate.DirtyStateError
	if !errors.As(err, &dirtyErr) {
		t.Fatalf("expected a *DirtyStateError, got %v", err)
	}
	if dirtyErr.Table != table {
		t.Fatalf("expected DirtyStateError.Table = %q, got %q", table, dirtyErr.Table)
	}
}
