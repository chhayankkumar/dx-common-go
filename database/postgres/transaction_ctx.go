// Transaction propagation + retry (W9b, GO-STANDARDS-ROLLOUT_PLAN Part B R3):
// business services call InTransaction once at the top of a use case; nested
// calls that also use InTransaction JOIN the caller's transaction via context
// instead of opening a second one — no tx boilerplate in service code.
package postgres

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// TxFromContext returns the transaction propagated by InTransaction, if any.
// Repos can use it to bind a DAO: dao.WithTx(tx).
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// InTransaction runs fn inside a transaction with context propagation:
// if ctx already carries a transaction (a caller higher up used
// InTransaction), fn JOINS it — commit/rollback stay with the outermost
// caller. Otherwise a new transaction is begun, committed on nil error and
// rolled back on error (same semantics as WithTransaction).
func InTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(ctx, tx) // join the outer transaction
	}
	return WithTransaction(ctx, pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx), tx)
	})
}

// retryable PostgreSQL error codes: serialization_failure, deadlock_detected.
func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

// WithRetryableTx runs fn in a transaction and retries the WHOLE transaction
// (bounded, jittered backoff) when PostgreSQL reports a serialization failure
// or deadlock — fn must therefore be safe to re-run. Other errors are not
// retried. maxAttempts <= 0 defaults to 3.
func WithRetryableTx(ctx context.Context, pool *pgxpool.Pool, maxAttempts int, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var err error
	for attempt := 1; ; attempt++ {
		err = InTransaction(ctx, pool, fn)
		if err == nil || !isRetryable(err) || attempt >= maxAttempts {
			return err
		}
		backoff := time.Duration(attempt) * 50 * time.Millisecond
		backoff += time.Duration(rand.Int63n(int64(25 * time.Millisecond))) //nolint:gosec // jitter only
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}
