package sqlcx_test

// First test file for sqlcx (it had none before this round). dx-common-go
// has no sqlc-generated package of its own to exercise (the reference lives
// in dx-acl-go), so these tests prove DB(ctx, pool)'s actual contract
// directly against a real Postgres pool/transaction: it must return the
// pool when ctx carries no ambient transaction, and the real transaction
// when one is present — provable by whether an insert survives a deliberate
// rollback.

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/datakaveri/dx-common-go/database/postgres/sqlcx"
	dxtx "github.com/datakaveri/dx-common-go/database/postgres/transaction"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
	"github.com/datakaveri/dx-common-go/dxtest/fixtures"
)

func TestDB_ReturnsPoolWhenNoAmbientTransaction(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id := "sqlcx-pool-1"
	t.Cleanup(func() { h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	q := sqlcx.DB(ctx, h.Pool)
	if _, err := q.Exec(ctx, "INSERT INTO widgets (id, name, quantity) VALUES ($1, $2, $3)", id, "n", 1); err != nil {
		t.Fatalf("exec via DB(ctx, pool): %v", err)
	}

	var exists bool
	if err := h.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check exists: %v", err)
	}
	if !exists {
		t.Fatal("expected the insert (via the pool-routed Querier) to be immediately visible, it wasn't")
	}
}

func TestDB_ReturnsAmbientTransactionWhenPresent(t *testing.T) {
	h := containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir))
	ctx := context.Background()
	id := "sqlcx-tx-1"
	t.Cleanup(func() { h.Pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	wantErr := errors.New("force rollback")
	err := dxtx.InTransaction(ctx, h.Pool, func(txCtx context.Context, _ pgx.Tx) error {
		q := sqlcx.DB(txCtx, h.Pool)
		if _, err := q.Exec(txCtx, "INSERT INTO widgets (id, name, quantity) VALUES ($1, $2, $3)", id, "n", 1); err != nil {
			return err
		}
		return wantErr
	})
	if err == nil {
		t.Fatal("expected the wrapped fn error to propagate")
	}

	// If DB had returned the pool instead of the ambient tx, this insert
	// would have committed independently and leaked past the rollback.
	var exists bool
	if err := h.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check exists: %v", err)
	}
	if exists {
		t.Fatal("expected DB(ctx, pool) to have returned the ambient transaction and rolled back with it, but the row is visible")
	}
}

func TestDB_QuerierInterfaceSatisfiesBothPoolAndTx(t *testing.T) {
	h := containers.Postgres(t)
	ctx := context.Background()

	var viaPool int
	if err := sqlcx.DB(ctx, h.Pool).QueryRow(ctx, "SELECT 1").Scan(&viaPool); err != nil {
		t.Fatalf("query via pool-routed Querier: %v", err)
	}
	if viaPool != 1 {
		t.Fatalf("expected 1, got %d", viaPool)
	}

	err := dxtx.InTransaction(ctx, h.Pool, func(txCtx context.Context, _ pgx.Tx) error {
		var viaTx int
		if err := sqlcx.DB(txCtx, h.Pool).QueryRow(txCtx, "SELECT 1").Scan(&viaTx); err != nil {
			return err
		}
		if viaTx != 1 {
			t.Fatalf("expected 1, got %d", viaTx)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTransaction: %v", err)
	}
}
