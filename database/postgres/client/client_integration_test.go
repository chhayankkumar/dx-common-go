package client_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	dxclient "github.com/datakaveri/dx-common-go/database/postgres/client"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
)

func TestNewPool_ConnectsAndPings(t *testing.T) {
	h := containers.Postgres(t)

	if err := h.Pool.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	cfg := h.Pool.Config()
	if cfg.MaxConns <= 0 {
		t.Fatalf("expected a positive pool MaxConns, got %d", cfg.MaxConns)
	}
}

func TestNewPool_InvalidDSN_ReturnsError(t *testing.T) {
	_, err := dxclient.NewPool(dxclient.Config{DSN: "not-a-valid-dsn://:::"})
	if err == nil {
		t.Fatal("expected an error for an invalid DSN, got nil")
	}
}

// spyTracer records every TraceQueryStart/TraceQueryEnd call it receives, so
// tests can prove WithTracers actually wires a caller-supplied tracer into
// the real pool rather than silently dropping it.
type spyTracer struct {
	mu      sync.Mutex
	starts  int
	ends    int
	lastSQL string
}

func (s *spyTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	s.lastSQL = data.SQL
	return ctx
}

func (s *spyTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends++
}

func (s *spyTracer) counts() (starts, ends int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts, s.ends
}

func TestWithTracers_MultiTracerFansOutToSpy(t *testing.T) {
	dsn := containers.Postgres(t).DSN

	spy := &spyTracer{}
	pool, err := dxclient.NewPool(dxclient.Config{DSN: dsn}, dxclient.WithTracers(spy))
	if err != nil {
		t.Fatalf("NewPool with tracers: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	starts, ends := spy.counts()
	if starts != 1 || ends != 1 {
		t.Fatalf("expected the spy tracer to see exactly one start/end pair, got starts=%d ends=%d", starts, ends)
	}
}

func TestSlowQueryTracer_LogsOnlyOverThreshold(t *testing.T) {
	dsn := containers.Postgres(t).DSN

	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	pool, err := dxclient.NewPool(dxclient.Config{DSN: dsn}, dxclient.WithTracers(&dxclient.SlowQueryTracer{
		Threshold: 10 * time.Millisecond,
		Logger:    logger,
	}))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatalf("fast query: %v", err)
	}
	if _, err := pool.Exec(ctx, "SELECT pg_sleep(0.05)"); err != nil {
		t.Fatalf("slow query: %v", err)
	}

	entries := logs.All()
	var sawSlow, sawFast bool
	for _, e := range entries {
		if e.Message != "slow query" {
			continue
		}
		for _, f := range e.Context {
			if f.Key != "sql" {
				continue
			}
			if f.String == "SELECT pg_sleep(0.05)" {
				sawSlow = true
			}
			if f.String == "SELECT 1" {
				sawFast = true
			}
		}
	}
	if !sawSlow {
		t.Fatal("expected the slow query to be logged, it wasn't")
	}
	if sawFast {
		t.Fatal("expected the fast query NOT to be logged, but it was")
	}
}
