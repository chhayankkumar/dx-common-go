package migrate

import (
	"embed"
	"strings"
	"testing"
)

//go:embed testdata
var testFS embed.FS

func TestRun_ModeNoneIsNoOp(t *testing.T) {
	// No DSN dial should ever happen in ModeNone — an obviously-invalid DSN
	// proves Run returned before touching the network.
	cfg := Config{Mode: ModeNone, DSN: "postgres://invalid:invalid@127.0.0.1:1/nope", TableName: "schema_migrations_test"}
	if err := Run(cfg, testFS, "testdata", nil); err != nil {
		t.Fatalf("Run with ModeNone must be a no-op, got: %v", err)
	}
}

func TestDirtyStateError_Message(t *testing.T) {
	err := &DirtyStateError{Version: 3, Table: "schema_migrations_acl"}
	msg := err.Error()
	for _, want := range []string{"schema_migrations_acl", "dirty", "version 3", "migrate force 3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("DirtyStateError message missing %q: %s", want, msg)
		}
	}
}
