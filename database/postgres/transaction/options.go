package transaction

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	rc := RetryConfig{MaxAttempts: 3, BaseDelay: 20 * time.Millisecond}
	if len(cfg) > 0 {
		if cfg[0].MaxAttempts > 0 {
			rc.MaxAttempts = cfg[0].MaxAttempts
		}
		if cfg[0].BaseDelay > 0 {
			rc.BaseDelay = cfg[0].BaseDelay
		}
	}

	var lastErr error
	delay := rc.BaseDelay
	for attempt := 1; attempt <= rc.MaxAttempts; attempt++ {
		lastErr = WithTransaction(ctx, pool, fn)
		if lastErr == nil || !isRetryablePgError(lastErr) {
			return lastErr
		}
		if attempt == rc.MaxAttempts {
			break
		}
		jitter := time.Duration(rand.Int63n(int64(delay) + 1))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay + jitter):
		}
		delay *= 2
	}
	return fmt.Errorf("transaction.WithRetryableTx: giving up after %d attempts: %w", rc.MaxAttempts, lastErr)
}

func isRetryablePgError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrSerialization || pgErr.Code == pgErrDeadlock
}
