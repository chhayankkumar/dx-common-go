package repository_test

// Real-Postgres integration tests for the repository package, proving the
// promoted CRUD/paging/batch passthroughs and the ambient-transaction
// binding rule work end to end — complementing options_test.go/sql_test.go,
// which only exercise construction and DAO-less code paths.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/datakaveri/dx-common-go/database/postgres/dao"
	"github.com/datakaveri/dx-common-go/database/postgres/query"
	"github.com/datakaveri/dx-common-go/database/postgres/repository"
	dxtx "github.com/datakaveri/dx-common-go/database/postgres/transaction"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
	"github.com/datakaveri/dx-common-go/dxtest/fixtures"
)

// repoWidget mirrors every column in the shared widgets fixture table —
// pgx.RowToStructByNameLax tolerates a struct with MORE fields than the row
// selects, but not fewer, and InsertMap/RETURNING * always returns every
// column.
type repoWidget struct {
	ID         string     `db:"id"`
	Name       string     `db:"name"`
	Status     string     `db:"status"`
	CategoryID *string    `db:"category_id"`
	Quantity   int        `db:"quantity"`
	Version    int64      `db:"version"`
	CreatedAt  time.Time  `db:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at"`
	CreatedBy  *string    `db:"created_by"`
	UpdatedBy  *string    `db:"updated_by"`
	DeletedAt  *time.Time `db:"deleted_at"`
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir)).Pool
}

func TestBase_New_WithTable_CRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", "r-crud-1") })

	repo := repository.New[repoWidget](pool, repository.WithTable[repoWidget]("widgets"), repository.WithID[repoWidget]("id"))

	inserted, err := repo.InsertMap(ctx, map[string]any{"id": "r-crud-1", "name": "n", "quantity": 1})
	if err != nil {
		t.Fatalf("InsertMap: %v", err)
	}
	if inserted.Quantity != 1 {
		t.Fatalf("unexpected inserted row: %+v", inserted)
	}

	found, err := repo.FindByID(ctx, "r-crud-1")
	if err != nil || found.Name != "n" {
		t.Fatalf("FindByID: %+v, err=%v", found, err)
	}

	if err := repo.Update(ctx, map[string]any{"quantity": 9}, []query.Condition{query.Eq("id", "r-crud-1")}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.SoftDelete(ctx, "r-crud-1"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
}

func TestBase_DAO_BindsAmbientTransaction(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id := "r-ambient-1"
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	repo := repository.New[repoWidget](pool, repository.WithTable[repoWidget]("widgets"), repository.WithID[repoWidget]("id"))

	wantErr := errors.New("force rollback")
	err := dxtx.InTransaction(ctx, pool, func(txCtx context.Context, _ pgx.Tx) error {
		if _, err := repo.InsertMap(txCtx, map[string]any{"id": id, "name": "n", "quantity": 1}); err != nil {
			return err
		}
		return wantErr
	})
	if err == nil {
		t.Fatal("expected the wrapped fn error to propagate")
	}

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM widgets WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check exists: %v", err)
	}
	if exists {
		t.Fatal("expected Base.DAO(ctx) to have joined the ambient transaction and rolled back with it, but the row is visible")
	}
}

func TestBase_FindPage_And_Count_Pagination(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := repository.New[repoWidget](pool, repository.WithTable[repoWidget]("widgets"), repository.WithID[repoWidget]("id"))

	ids := []string{"r-page-1", "r-page-2", "r-page-3"}
	t.Cleanup(func() {
		for _, id := range ids {
			pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id)
		}
	})
	for _, id := range ids {
		if _, err := repo.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": 1}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	cond := []query.Condition{query.Like("id", "r-page-%")}
	count, err := repo.Count(ctx, cond)
	if err != nil || count != 3 {
		t.Fatalf("Count: %d, err=%v", count, err)
	}
	page, err := repo.FindPage(ctx, cond, nil, 2, 0)
	if err != nil || page.Total != 3 || len(page.Data) != 2 {
		t.Fatalf("FindPage: %+v, err=%v", page, err)
	}
}

func TestBase_InsertMany_CopyFrom_UpdateByIDs_DeleteByIDs(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := repository.New[repoWidget](pool, repository.WithTable[repoWidget]("widgets"), repository.WithID[repoWidget]("id"))

	imIDs := []string{"r-im-1", "r-im-2"}
	cfIDs := []string{"r-cf-1", "r-cf-2"}
	t.Cleanup(func() {
		for _, id := range append(imIDs, cfIDs...) {
			pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id)
		}
	})

	if err := repo.InsertMany(ctx, []string{"id", "name", "quantity"}, [][]any{
		{imIDs[0], "n1", 1}, {imIDs[1], "n2", 2},
	}); err != nil {
		t.Fatalf("InsertMany: %v", err)
	}
	copied, err := repo.CopyFrom(ctx, []string{"id", "name", "quantity"}, [][]any{
		{cfIDs[0], "n1", 1}, {cfIDs[1], "n2", 2},
	})
	if err != nil || copied != 2 {
		t.Fatalf("CopyFrom: copied=%d err=%v", copied, err)
	}

	if err := repo.UpdateByIDs(ctx, imIDs, map[string]any{"quantity": 42}); err != nil {
		t.Fatalf("UpdateByIDs: %v", err)
	}
	updated, err := repo.FindByIDs(ctx, imIDs)
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	for _, w := range updated {
		if w.Quantity != 42 {
			t.Fatalf("expected quantity=42 after UpdateByIDs, got %+v", w)
		}
	}

	if err := repo.DeleteByIDs(ctx, cfIDs); err != nil {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	remaining, err := repo.FindByIDs(ctx, cfIDs)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("expected DeleteByIDs to remove both rows, got %+v, err=%v", remaining, err)
	}
}

func TestBase_Unscoped_BypassesSoftDeleteFilter(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	id := "r-unscoped-1"
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM widgets WHERE id = $1", id) })

	repo := repository.New[repoWidget](pool,
		repository.WithTable[repoWidget]("widgets"), repository.WithID[repoWidget]("id"),
		repository.WithDAOOption[repoWidget](dao.WithSoftDeleteFilter[repoWidget]("status")))

	if _, err := repo.InsertMap(ctx, map[string]any{"id": id, "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.SoftDelete(ctx, id); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	if _, err := repo.FindByID(ctx, id); err == nil {
		t.Fatal("expected FindByID to miss the soft-deleted row through the default scope")
	}
	if _, err := repo.Unscoped().FindByID(ctx, id); err != nil {
		t.Fatalf("expected Unscoped().FindByID to still find it, got %v", err)
	}
}
