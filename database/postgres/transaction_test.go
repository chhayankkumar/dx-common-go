package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryablePgError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"serialization failure", &pgconn.PgError{Code: pgErrSerialization}, true},
		{"deadlock", &pgconn.PgError{Code: pgErrDeadlock}, true},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"non-pg error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryablePgError(tc.err); got != tc.want {
				t.Errorf("isRetryablePgError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// dummyTx satisfies pgx.Tx via embedding (all methods nil-panic if called) —
// enough to test InTransaction's context-propagation identity without a
// real database.
type dummyTx struct{ pgx.Tx }

func TestTxFromContext_AbsentByDefault(t *testing.T) {
	if _, ok := TxFromContext(context.Background()); ok {
		t.Fatal("a plain context should not carry a transaction")
	}
}

func TestInTransaction_JoinsAmbientTxWithoutTouchingPool(t *testing.T) {
	outer := pgx.Tx(&dummyTx{})
	ctx := context.WithValue(context.Background(), txContextKey{}, outer)

	var gotTx pgx.Tx
	// pool is deliberately nil: if InTransaction failed to detect the
	// ambient tx and tried pool.Begin instead, this would panic on the nil
	// pointer, not just fail an assertion.
	err := InTransaction(ctx, nil, func(_ context.Context, tx pgx.Tx) error {
		gotTx = tx
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTx != outer {
		t.Fatal("InTransaction did not pass through the ambient transaction")
	}
}

func TestInTransaction_PropagatesTxViaContext(t *testing.T) {
	outer := pgx.Tx(&dummyTx{})
	ctx := context.WithValue(context.Background(), txContextKey{}, outer)

	err := InTransaction(ctx, nil, func(innerCtx context.Context, _ pgx.Tx) error {
		tx, ok := TxFromContext(innerCtx)
		if !ok || tx != outer {
			t.Fatal("fn's context should carry the same ambient transaction via TxFromContext")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
