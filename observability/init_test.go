package observability

import (
	"context"
	"os"
	"testing"
)

// TestInit_NoOpWithoutEndpoint pins the safe-by-default contract: with no
// Endpoint and no OTEL_EXPORTER_OTLP_ENDPOINT set, Init must not error and
// must return a usable (no-op) shutdown — the "call unconditionally, zero
// risk" guarantee services rely on.
func TestInit_NoOpWithoutEndpoint(t *testing.T) {
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); ok {
		os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		defer os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", v)
	}

	shutdown, err := Init(context.Background(), Config{ServiceName: "test-svc"})
	if err != nil {
		t.Fatalf("Init returned error in no-op mode: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned a nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

// TestInit_SecondCallIsNoOp pins that a second Init call in the same process
// never panics or double-initializes the global TracerProvider — it must
// return a safe no-op rather than re-running SDK setup.
func TestInit_SecondCallIsNoOp(t *testing.T) {
	_, _ = Init(context.Background(), Config{ServiceName: "first"})
	shutdown, err := Init(context.Background(), Config{ServiceName: "second"})
	if err != nil {
		t.Fatalf("second Init call returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("second Init call returned a nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("second call's shutdown returned error: %v", err)
	}
}
