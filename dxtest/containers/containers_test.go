package containers

import (
	"testing"
	"testing/fstest"
)

// TestWithMigrations_SetsConfig pins that WithMigrations accumulates onto
// postgresConfig correctly — the only part of Postgres(t, ...) testable
// without a live Postgres (container or DSN).
func TestWithMigrations_SetsConfig(t *testing.T) {
	fsys := fstest.MapFS{"migrations/0001_x.up.sql": {Data: []byte("SELECT 1;")}}
	cfg := &postgresConfig{}
	WithMigrations(fsys, "migrations")(cfg)

	if cfg.migrations == nil {
		t.Fatal("WithMigrations did not set migrations fs.FS")
	}
	if cfg.migrationsDir != "migrations" {
		t.Fatalf("migrationsDir = %q, want %q", cfg.migrationsDir, "migrations")
	}
}

func TestTrimRedisScheme(t *testing.T) {
	cases := []struct{ in, want string }{
		{"redis://127.0.0.1:6379", "127.0.0.1:6379"},
		{"redis://localhost:6379/0", "localhost:6379/0"},
		{"127.0.0.1:6379", "127.0.0.1:6379"}, // no scheme: unchanged
		{"", ""},
	}
	for _, tc := range cases {
		if got := trimRedisScheme(tc.in); got != tc.want {
			t.Errorf("trimRedisScheme(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
