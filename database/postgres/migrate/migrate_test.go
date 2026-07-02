package migrate

import (
	"testing"
	"testing/fstest"

	"go.uber.org/zap"
)

var fakeFS = fstest.MapFS{
	"migrations/0001_baseline.up.sql":   {Data: []byte("CREATE TABLE t (id int);")},
	"migrations/0001_baseline.down.sql": {Data: []byte("DROP TABLE t;")},
}

// TestModeNoneSkips pins the mode gate: none must run no DDL and succeed
// without a database.
func TestModeNoneSkips(t *testing.T) {
	if err := Run(Config{Mode: ModeNone}, fakeFS, "migrations", zap.NewNop()); err != nil {
		t.Fatalf("mode=none must be a no-op, got %v", err)
	}
}

func TestUnknownModeRejected(t *testing.T) {
	if err := Run(Config{Mode: "auto"}, fakeFS, "migrations", zap.NewNop()); err == nil {
		t.Fatal("unknown mode must be rejected")
	}
}

func TestMissingDSNRejected(t *testing.T) {
	if err := Run(Config{Mode: ModeMigrate}, fakeFS, "migrations", zap.NewNop()); err == nil {
		t.Fatal("empty DSN must be rejected before attempting to connect")
	}
}
