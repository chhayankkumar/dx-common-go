package client

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolOption configures the pgxpool.Config NewPool builds, for capabilities
// that don't belong in Config (e.g. WithTracers) — additive: every existing
// two-argument NewPool(cfg) call site compiles unchanged.
type PoolOption func(*pgxpool.Config)

// WithTracers installs a pgx.QueryTracer composed from tracers (via
// MultiTracer) on the pool — pgxpool.Config has room for exactly one
// Tracer, so composing observability concerns (OTel spans, slow-query
// logging, metrics) means combining them here rather than each overwriting
// the others. Calling WithTracers more than once keeps only the last call's
// tracers (pgxpool.Config.Tracer is a single field); pass every tracer to
// one call.
func WithTracers(tracers ...pgx.QueryTracer) PoolOption {
	return func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Tracer = NewMultiTracer(tracers...)
	}
}

// NewPool creates and validates a pgxpool.Pool using the provided Config.
// It pings the server before returning to surface connectivity issues early.
func NewPool(cfg Config, opts ...PoolOption) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("client.NewPool: parsing DSN: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	for _, opt := range opts {
		opt(poolCfg)
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = 10 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("client.NewPool: creating pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("client.NewPool: ping failed: %w", err)
	}

	return pool, nil
}
