package transaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/datakaveri/dx-common-go/resilience"
)

// pgErrSerialization / pgErrDeadlock are the PostgreSQL error codes
// WithRetryableTx treats as transient and safe to retry. Duplicated from
// dao's private constants rather than exported cross-package, since they're
// well-known, stable PostgreSQL codes, not something either package owns.
const (
	pgErrSerialization = "40001"
	pgErrDeadlock      = "40P01"
)

// RetryConfig tunes WithRetryableTx's retry policy.
type RetryConfig struct {
	// MaxAttempts is the total number of tries (including the first).
	// Defaults to 3.
	MaxAttempts int
	// BaseDelay is the starting backoff before the second attempt; each
	// subsequent attempt doubles it, plus up to BaseDelay of jitter.
	// Defaults to 20ms.
	BaseDelay time.Duration
}

// WithRetryableTx runs WithTransaction, retrying fn from scratch (a fresh
// transaction each time) when it fails with a serialization failure (40001)
// or deadlock (40P01) — the two PostgreSQL errors that mean "no fault of
// yours, just try again." Any other error, or exhausting MaxAttempts,
// returns immediately.
func WithRetryableTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error, cfg ...RetryConfig) error {
	return retryTx(ctx, "WithRetryableTx", resolveRetryConfig(cfg), func(ctx context.Context) error {
		return WithTransaction(ctx, pool, fn)
	})
}

// resolveRetryConfig applies the defaults (3 attempts, 20ms base) plus any
// caller override.
func resolveRetryConfig(cfg []RetryConfig) RetryConfig {
	rc := RetryConfig{MaxAttempts: 3, BaseDelay: 20 * time.Millisecond}
	if len(cfg) > 0 {
		if cfg[0].MaxAttempts > 0 {
			rc.MaxAttempts = cfg[0].MaxAttempts
		}
		if cfg[0].BaseDelay > 0 {
			rc.BaseDelay = cfg[0].BaseDelay
		}
	}
	return rc
}

// retryTx runs a transaction closure under the shared resilience.Retry engine,
// retrying only serialization/deadlock failures (40001/40P01) with exponential
// backoff + jitter, and preserving the "giving up after N attempts" wrap when
// retries are exhausted on a retryable error. label names the caller for that
// error.
func retryTx(ctx context.Context, label string, rc RetryConfig, run func(context.Context) error) error {
	policy := resilience.NewPolicy(
		resilience.WithMaxAttempts(rc.MaxAttempts),
		resilience.WithBaseDelay(rc.BaseDelay),
		resilience.WithMultiplier(2),
	)
	err := resilience.Retry(ctx, policy, run, resilience.WithRetryable(isRetryablePgError))
	if err != nil && isRetryablePgError(err) {
		return fmt.Errorf("transaction.%s: giving up after %d attempts: %w", label, rc.MaxAttempts, err)
	}
	return err
}

func isRetryablePgError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrSerialization || pgErr.Code == pgErrDeadlock
}
