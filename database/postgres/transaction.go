package postgres

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

// WithTransaction begins a transaction, executes fn, and commits on success.
// If fn returns an error the transaction is rolled back and the error is
// propagated. The rollback error, if any, is wrapped with the original error.
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	if fnErr := fn(tx); fnErr != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("transaction fn error: %w; rollback error: %v", fnErr, rbErr)
		}
		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// txContextKey is unexported so only this package can stash/retrieve the
// ambient transaction — callers only ever see it through TxFromContext.
type txContextKey struct{}

// TxFromContext returns the transaction InTransaction stashed on ctx, if any.
// Repositories use this to bind to an ambient transaction when present and
// fall back to the pool otherwise, so a caller several layers up can compose
// multiple repository calls into one atomic unit without threading a pgx.Tx
// through every signature.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}

// InTransaction runs fn inside a transaction, propagated via ctx (see
// TxFromContext) so that repository methods several calls deep can join it
// automatically instead of requiring every layer to pass a pgx.Tx by hand.
//
// If ctx already carries a transaction (a nested InTransaction call), fn
// joins that transaction directly — only the outermost call begins, commits,
// or rolls back. This lets a service compose several repository writes that
// each independently support "run inside a transaction" into a single atomic
// unit just by wrapping them in one outer InTransaction call, with no
// special-casing at the call sites for "am I nested."
func InTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(ctx, tx)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)
	if fnErr := fn(txCtx, tx); fnErr != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("transaction fn error: %w; rollback error: %v", fnErr, rbErr)
		}
		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

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
	return fmt.Errorf("postgres.WithRetryableTx: giving up after %d attempts: %w", rc.MaxAttempts, lastErr)
}

func isRetryablePgError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrSerialization || pgErr.Code == pgErrDeadlock
}

// WithAdvisoryLock acquires a session-level PostgreSQL advisory lock on key
// for the duration of fn, using a single connection checked out from pool
// (advisory locks are session-scoped, so the same physical connection must
// hold the lock and release it). Useful for in-process singleton work
// (a cron-like poller that must not run concurrently across replicas)
// without a separate coordination service.
func WithAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, key int64, fn func() error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("postgres.WithAdvisoryLock: acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		return fmt.Errorf("postgres.WithAdvisoryLock: lock: %w", err)
	}
	defer func() {
		// Best-effort unlock on the same connection; conn.Release above
		// returns it to the pool regardless, but PostgreSQL also releases
		// session-level advisory locks automatically when the connection
		// closes, so a failed unlock here is not a leak.
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key); err != nil {
			_ = err
		}
	}()

	return fn()
}

// InRetryableTransaction is InTransaction plus WithRetryableTx's retry
// policy: fn runs inside a context-propagated transaction (repositories
// join it via TxFromContext), and the WHOLE transaction is retried from
// scratch on serialization failure (40001) or deadlock (40P01) with
// doubling backoff + jitter. fn must therefore be safe to re-run.
//
// When ctx already carries a transaction (nested call), fn simply joins it
// and NO retry is attempted here — a broken outer transaction cannot be
// salvaged from inside; the outermost owner retries.
func InRetryableTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error, cfg ...RetryConfig) error {
	if _, ok := TxFromContext(ctx); ok {
		return InTransaction(ctx, pool, fn)
	}

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
		lastErr = InTransaction(ctx, pool, fn)
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
	return fmt.Errorf("postgres.InRetryableTransaction: giving up after %d attempts: %w", rc.MaxAttempts, lastErr)
}
