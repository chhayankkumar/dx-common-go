package transaction_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dxtx "github.com/datakaveri/dx-common-go/database/postgres/transaction"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
	"github.com/datakaveri/dx-common-go/dxtest/fixtures"
)

// insertWidget returns its error rather than calling t.Fatalf — it runs
// inside transaction.WithTransaction/InTransaction closures in several
// tests below, and t.Fatalf's runtime.Goexit unwinds the current goroutine
// without returning control to the caller, so the transaction manager never
// sees the failure, never rolls back, and the checked-out pool connection
// is held forever (observed directly: it hung the whole test binary on
// pool.Close's cleanup until the 10-minute default timeout).
func insertWidget(ctx context.Context, tx pgx.Tx, id string, qty int) error {
	_, err := tx.Exec(ctx,
		"INSERT INTO widgets (id, name, quantity) VALUES ($1, $2, $3)", id, "w-"+id, qty)
	return err
}

func widgetExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check widget %s exists: %v", id, err)
	}
	return exists
}

func TestWithTransaction_CommitsOnSuccess(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id := "tx-commit-1"
	t.Cleanup(func() { h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	err := dxtx.WithTransaction(ctx, h.Pool, func(tx pgx.Tx) error {
		return insertWidget(ctx, tx, id, 1)
	})
	if err != nil {
		t.Fatalf("WithTransaction: %v", err)
	}

	if !widgetExists(t, ctx, h.Pool, id) {
		t.Fatal("expected the row to be visible after a successful commit, it wasn't")
	}
}

func TestWithTransaction_RollsBackOnError(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id := "tx-rollback-1"
	t.Cleanup(func() { h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	wantErr := errors.New("boom")
	err := dxtx.WithTransaction(ctx, h.Pool, func(tx pgx.Tx) error {
		if err := insertWidget(ctx, tx, id, 1); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the fn error to propagate, got %v", err)
	}

	if widgetExists(t, ctx, h.Pool, id) {
		t.Fatal("expected the row to be absent after a rollback, it was visible")
	}
}

func TestInTransaction_NestedCallsShareOneRealTransaction(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id1, id2 := "tx-nested-1", "tx-nested-1-dup" // id2 distinct; failure forced via duplicate PK below
	t.Cleanup(func() {
		h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id1)
		h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id2)
	})

	err := dxtx.InTransaction(ctx, h.Pool, func(outerCtx context.Context, _ pgx.Tx) error {
		if err := dxtx.InTransaction(outerCtx, h.Pool, func(innerCtx context.Context, tx pgx.Tx) error {
			return insertWidget(innerCtx, tx, id1, 1)
		}); err != nil {
			return err
		}
		// Second inner call fails (duplicate PK) — since both inner calls
		// join the SAME outer transaction (proved by TxFromContext), the
		// whole thing must roll back, undoing the first insert too.
		return dxtx.InTransaction(outerCtx, h.Pool, func(innerCtx context.Context, tx pgx.Tx) error {
			return insertWidget(innerCtx, tx, id1, 2) // same id1 -> unique violation
		})
	})
	if err == nil {
		t.Fatal("expected the duplicate-PK insert to fail, it didn't")
	}

	if widgetExists(t, ctx, h.Pool, id1) {
		t.Fatal("expected the first insert to have rolled back along with the second failure, but it's visible")
	}
}

func TestInRetryableTransaction_RetriesOnSerializationFailure(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id := "tx-serializable-race"
	if _, err := h.Pool.Exec(ctx, "INSERT INTO widgets (id, name, quantity) VALUES ($1, $2, 0)", id, "race"); err != nil {
		t.Fatalf("seed widget: %v", err)
	}
	t.Cleanup(func() { h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	incrementer := func() error {
		return dxtx.InRetryableTransaction(ctx, h.Pool, func(_ context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"); err != nil {
				return err
			}
			var qty int
			if err := tx.QueryRow(ctx, "SELECT quantity FROM widgets WHERE id = $1", id).Scan(&qty); err != nil {
				return err
			}
			// Give the other goroutine's concurrent transaction time to also
			// read before either of us writes — maximizes the chance both
			// transactions overlap and Postgres detects a real read/write
			// conflict under SERIALIZABLE, forcing a genuine 40001.
			time.Sleep(30 * time.Millisecond)
			_, err := tx.Exec(ctx, "UPDATE widgets SET quantity = $1 WHERE id = $2", qty+1, id)
			return err
		}, dxtx.RetryConfig{MaxAttempts: 8, BaseDelay: 10 * time.Millisecond})
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = incrementer()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: expected eventual success within the retry budget, got %v", i, err)
		}
	}

	var final int
	if err := h.Pool.QueryRow(ctx, "SELECT quantity FROM widgets WHERE id = $1", id).Scan(&final); err != nil {
		t.Fatalf("read final quantity: %v", err)
	}
	if final != 2 {
		t.Fatalf("expected both increments to land (quantity=2), got %d", final)
	}
}

func TestWithAdvisoryLock_SerializesConcurrentCallers(t *testing.T) {
	h := containers.Postgres(t)
	ctx := context.Background()
	const key = int64(424242)

	var mu sync.Mutex
	var intervals [][2]time.Time

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			err := dxtx.WithAdvisoryLock(ctx, h.Pool, key, func() error {
				start := time.Now()
				time.Sleep(30 * time.Millisecond)
				end := time.Now()
				mu.Lock()
				intervals = append(intervals, [2]time.Time{start, end})
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithAdvisoryLock: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(intervals) != 2 {
		t.Fatalf("expected 2 recorded intervals, got %d", len(intervals))
	}
	if intervalsOverlap(intervals[0], intervals[1]) {
		t.Fatal("expected the two critical sections to be mutually exclusive, but they overlapped")
	}
}

func TestWithAdvisoryLock_DifferentKeysDoNotSerialize(t *testing.T) {
	h := containers.Postgres(t)
	ctx := context.Background()

	var mu sync.Mutex
	var intervals [][2]time.Time

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		key := int64(1000 + i) // distinct keys -> should NOT serialize
		go func(key int64) {
			defer wg.Done()
			err := dxtx.WithAdvisoryLock(ctx, h.Pool, key, func() error {
				start := time.Now()
				time.Sleep(50 * time.Millisecond)
				end := time.Now()
				mu.Lock()
				intervals = append(intervals, [2]time.Time{start, end})
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("WithAdvisoryLock: %v", err)
			}
		}(key)
	}
	wg.Wait()

	if len(intervals) != 2 {
		t.Fatalf("expected 2 recorded intervals, got %d", len(intervals))
	}
	if !intervalsOverlap(intervals[0], intervals[1]) {
		t.Fatal("expected the two critical sections (different keys) to overlap as a negative control, but they didn't — this test would pass even if locking were a no-op otherwise")
	}
}

func intervalsOverlap(a, b [2]time.Time) bool {
	return a[0].Before(b[1]) && b[0].Before(a[1])
}
