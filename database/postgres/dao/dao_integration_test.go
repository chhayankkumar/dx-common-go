package dao_test

// Real-Postgres integration tests for the dao package, against the shared
// dxtest/fixtures schema (widgets/categories). Every test runs inside its
// own rolled-back transaction (beginTx) so nothing is ever actually
// committed against the shared tables — full isolation without a
// truncate/reset step between tests. Complements the existing fake-Querier
// unit tests (base_test.go et al.), which pin SQL-rendering/argument
// threading without a real database; these tests prove the SQL actually
// executes and real pgx scanning/Postgres error codes behave as documented.

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/datakaveri/dx-common-go/database/postgres/dao"
	"github.com/datakaveri/dx-common-go/database/postgres/query"
	"github.com/datakaveri/dx-common-go/dxtest/containers"
	"github.com/datakaveri/dx-common-go/dxtest/fixtures"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// testWidget is the fixture entity for these integration tests — kept
// distinct from base_test.go's minimal fake `widget` (same package as
// production code would collide; this file is `dao_test`, an external test
// package, so there's no actual redeclaration risk, but the richer shape
// with all fixture columns earns its own name regardless).
type testWidget struct {
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

type testWidgetWithCategory struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	CategoryName string `db:"category_name"`
}

type categoryCount struct {
	CategoryID string `db:"category_id"`
	Count      int64  `db:"total"`
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return containers.Postgres(t, containers.WithSetupSQL(fixtures.FS, fixtures.Dir)).Pool
}

// beginTx starts a real transaction and rolls it back on cleanup — every
// test using it never actually commits against the shared fixture tables.
func beginTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func TestBaseDAO_InsertAndFindByID_RoundTrips(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	inserted, err := d.InsertMap(ctx, map[string]any{
		"id": "w1", "name": "Widget One", "quantity": 5,
	})
	if err != nil {
		t.Fatalf("InsertMap: %v", err)
	}
	if inserted.Status != "ACTIVE" || inserted.Quantity != 5 {
		t.Fatalf("unexpected inserted row: %+v", inserted)
	}

	found, err := d.FindByID(ctx, "w1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.Name != "Widget One" || found.Quantity != 5 || found.CreatedAt.IsZero() {
		t.Fatalf("unexpected round-tripped row: %+v", found)
	}
}

func TestBaseDAO_FindOne_And_FindAll_WithConditions(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	for _, w := range []struct {
		id  string
		qty int
	}{{"w-fa-1", 1}, {"w-fa-2", 2}, {"w-fa-3", 2}} {
		if _, err := d.InsertMap(ctx, map[string]any{"id": w.id, "name": w.id, "quantity": w.qty}); err != nil {
			t.Fatalf("seed %s: %v", w.id, err)
		}
	}

	one, err := d.FindOne(ctx, []query.Condition{query.Eq("id", "w-fa-1")})
	if err != nil || one.ID != "w-fa-1" {
		t.Fatalf("FindOne: %+v, err=%v", one, err)
	}

	all, err := d.FindAll(ctx, []query.Condition{query.Eq("quantity", 2)})
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows with quantity=2, got %d", len(all))
	}
}

func TestBaseDAO_Update_And_UpdateReturning(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	if _, err := d.InsertMap(ctx, map[string]any{"id": "w-upd", "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := d.UpdateReturning(ctx,
		map[string]any{"quantity": 9},
		[]query.Condition{query.Eq("id", "w-upd")})
	if err != nil {
		t.Fatalf("UpdateReturning: %v", err)
	}
	if updated.Quantity != 9 {
		t.Fatalf("expected quantity=9, got %d", updated.Quantity)
	}

	found, err := d.FindByID(ctx, "w-upd")
	if err != nil || found.Quantity != 9 {
		t.Fatalf("FindByID after update: %+v, err=%v", found, err)
	}
}

func TestBaseDAO_Upsert_InsertsThenUpdatesOnConflict(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	first, err := d.Upsert(ctx, map[string]any{"id": "w-up", "name": "n", "quantity": 1}, "id", []string{"quantity"})
	if err != nil || first.Quantity != 1 {
		t.Fatalf("first upsert: %+v, err=%v", first, err)
	}

	second, err := d.Upsert(ctx, map[string]any{"id": "w-up", "name": "n", "quantity": 42}, "id", []string{"quantity"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.Quantity != 42 {
		t.Fatalf("expected the conflict update to land, got quantity=%d", second.Quantity)
	}
}

func TestBaseDAO_UpdateVersioned_OptimisticLocking(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	if _, err := d.InsertMap(ctx, map[string]any{"id": "w-ver", "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	updated, err := d.UpdateVersioned(ctx, map[string]any{"quantity": 2}, nil, "version", 0)
	if err != nil {
		t.Fatalf("UpdateVersioned (expected=0): %v", err)
	}
	if updated.Version != 1 {
		t.Fatalf("expected version=1 after first versioned update, got %d", updated.Version)
	}

	_, err = d.UpdateVersioned(ctx, map[string]any{"quantity": 3},
		[]query.Condition{query.Eq("id", "w-ver")}, "version", 0)
	if !errors.Is(err, dao.ErrStaleVersion) {
		t.Fatalf("expected ErrStaleVersion on a stale retry, got %v", err)
	}
}

func TestBaseDAO_SoftDelete_And_Restore_RespectFilter(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAOWith[testWidget](tx, "widgets", dao.WithSoftDeleteFilter[testWidget]("status"))

	if _, err := d.InsertMap(ctx, map[string]any{"id": "w-sd", "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.SoftDelete(ctx, "w-sd"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	if _, err := d.FindByID(ctx, "w-sd"); !dxerrors.IsNotFoundError(err) {
		t.Fatalf("expected NotFound through the soft-delete filter, got %v", err)
	}
	if _, err := d.Unscoped().FindByID(ctx, "w-sd"); err != nil {
		t.Fatalf("Unscoped().FindByID should still find the soft-deleted row: %v", err)
	}

	if err := d.Restore(ctx, "w-sd"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := d.FindByID(ctx, "w-sd"); err != nil {
		t.Fatalf("expected FindByID to succeed after Restore, got %v", err)
	}
}

func TestBaseDAO_HardDelete_RemovesRow(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	if _, err := d.InsertMap(ctx, map[string]any{"id": "w-hd", "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.HardDelete(ctx, []query.Condition{query.Eq("id", "w-hd")}); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	exists, err := d.Exists(ctx, []query.Condition{query.Eq("id", "w-hd")})
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("expected the row to be gone after HardDelete")
	}
}

func TestBaseDAO_Exists_TrueAndFalse(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	if _, err := d.InsertMap(ctx, map[string]any{"id": "w-ex", "name": "n", "quantity": 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	present, err := d.Exists(ctx, []query.Condition{query.Eq("id", "w-ex")})
	if err != nil || !present {
		t.Fatalf("expected true, got present=%v err=%v", present, err)
	}
	absent, err := d.Exists(ctx, []query.Condition{query.Eq("id", "does-not-exist")})
	if err != nil || absent {
		t.Fatalf("expected false, got present=%v err=%v", absent, err)
	}
}

func TestBaseDAO_Count_And_FindPage_Pagination(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	for i := 0; i < 5; i++ {
		id := "w-page-" + string(rune('a'+i))
		if _, err := d.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": 1}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	cond := []query.Condition{query.Like("id", "w-page-%")}

	count, err := d.Count(ctx, cond)
	if err != nil || count != 5 {
		t.Fatalf("expected Count=5, got %d, err=%v", count, err)
	}

	page1, err := d.FindPage(ctx, cond, []query.OrderBy{{Column: "id"}}, 2, 0)
	if err != nil {
		t.Fatalf("FindPage page1: %v", err)
	}
	if page1.Total != 5 || len(page1.Data) != 2 || !page1.HasNext {
		t.Fatalf("unexpected page1: total=%d len=%d hasNext=%v", page1.Total, len(page1.Data), page1.HasNext)
	}

	pageLast, err := d.FindPage(ctx, cond, []query.OrderBy{{Column: "id"}}, 2, 4)
	if err != nil {
		t.Fatalf("FindPage last page: %v", err)
	}
	if pageLast.HasNext {
		t.Fatal("expected the last page to report HasNext=false")
	}
}

func TestBaseDAO_InsertMany_And_CopyFrom_BulkInsert(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	if err := d.InsertMany(ctx, []string{"id", "name", "quantity"}, [][]any{
		{"w-im-1", "n1", 1}, {"w-im-2", "n2", 2},
	}); err != nil {
		t.Fatalf("InsertMany: %v", err)
	}
	n, err := d.Count(ctx, []query.Condition{query.Like("id", "w-im-%")})
	if err != nil || n != 2 {
		t.Fatalf("expected 2 rows after InsertMany, got %d, err=%v", n, err)
	}

	copied, err := d.CopyFrom(ctx, []string{"id", "name", "quantity"}, [][]any{
		{"w-cf-1", "n1", 1}, {"w-cf-2", "n2", 2}, {"w-cf-3", "n3", 3},
	})
	if err != nil {
		t.Fatalf("CopyFrom: %v", err)
	}
	if copied != 3 {
		t.Fatalf("expected CopyFrom to report 3 rows, got %d", copied)
	}
	n, err = d.Count(ctx, []query.Condition{query.Like("id", "w-cf-%")})
	if err != nil || n != 3 {
		t.Fatalf("expected 3 rows after CopyFrom, got %d, err=%v", n, err)
	}
}

func TestBaseDAO_UpdateByIDs_And_DeleteByIDs_BulkByKey(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	ids := []string{"w-bulk-1", "w-bulk-2", "w-bulk-3"}
	for _, id := range ids {
		if _, err := d.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": 1}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	if err := d.UpdateByIDs(ctx, ids[:2], map[string]any{"quantity": 99}); err != nil {
		t.Fatalf("UpdateByIDs: %v", err)
	}
	got, err := d.FindByIDs(ctx, ids[:2])
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	for _, w := range got {
		if w.Quantity != 99 {
			t.Fatalf("expected quantity=99 after UpdateByIDs, got %d for %s", w.Quantity, w.ID)
		}
	}

	if err := d.DeleteByIDs(ctx, ids[:2]); err != nil {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	remaining, err := d.FindByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("FindByIDs after delete: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != ids[2] {
		t.Fatalf("expected only %s to remain, got %+v", ids[2], remaining)
	}
}

func TestBaseDAO_InsertIgnore_SkipsOnConflict(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	inserted, err := d.InsertIgnore(ctx, []string{"id", "name", "quantity"}, []any{"w-ii", "n", 1}, "id")
	if err != nil || !inserted {
		t.Fatalf("first InsertIgnore: inserted=%v err=%v", inserted, err)
	}
	inserted, err = d.InsertIgnore(ctx, []string{"id", "name", "quantity"}, []any{"w-ii", "n2", 2}, "id")
	if err != nil {
		t.Fatalf("second InsertIgnore: %v", err)
	}
	if inserted {
		t.Fatal("expected the second InsertIgnore to report inserted=false")
	}
	found, err := d.FindByID(ctx, "w-ii")
	if err != nil || found.Name != "n" {
		t.Fatalf("expected the original row untouched, got %+v, err=%v", found, err)
	}
}

func TestBaseDAO_WithActor_AutoPopulatesAuditColumns(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAOWith[testWidget](tx, "widgets", dao.WithAuditColumns[testWidget]("created_by", "updated_by"))

	ctx = dao.WithActor(ctx, "alice")
	inserted, err := d.InsertMap(ctx, map[string]any{"id": "w-audit", "name": "n", "quantity": 1})
	if err != nil {
		t.Fatalf("InsertMap: %v", err)
	}
	if inserted.CreatedBy == nil || *inserted.CreatedBy != "alice" {
		t.Fatalf("expected created_by=alice, got %v", inserted.CreatedBy)
	}
}

func TestBaseDAO_ConstraintViolations_MapToRightErrors(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	cases := []struct {
		name       string
		m          map[string]any
		wantStatus int
	}{
		{"not-null", map[string]any{"id": "w-nn", "quantity": 1}, http.StatusBadRequest},
		{"fk-violation", map[string]any{"id": "w-fk", "name": "n", "category_id": "does-not-exist"}, http.StatusBadRequest},
		{"check-violation", map[string]any{"id": "w-chk", "name": "n", "quantity": -1}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := beginTx(t, ctx, pool)
			d := dao.NewBaseDAO[testWidget](tx, "widgets")
			_, err := d.InsertMap(ctx, tc.m)
			assertDxStatus(t, err, tc.wantStatus)
		})
	}

	t.Run("unique-violation", func(t *testing.T) {
		tx := beginTx(t, ctx, pool)
		d := dao.NewBaseDAO[testWidget](tx, "widgets")
		if _, err := d.InsertMap(ctx, map[string]any{"id": "w-dup", "name": "n", "quantity": 1}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		_, err := d.InsertMap(ctx, map[string]any{"id": "w-dup", "name": "n2", "quantity": 2})
		assertDxStatus(t, err, http.StatusConflict)
	})
}

func assertDxStatus(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var dxErr dxerrors.DxError
	if !errors.As(err, &dxErr) {
		t.Fatalf("expected a dxerrors.DxError, got %v (%T)", err, err)
	}
	if dxErr.HTTPStatus() != wantStatus {
		t.Fatalf("expected HTTP status %d, got %d (%v)", wantStatus, dxErr.HTTPStatus(), err)
	}
}

func TestFinder_WhereOrderByLimitOffset_Find(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	for i := 1; i <= 3; i++ {
		id := "w-finder-" + string(rune('0'+i))
		if _, err := d.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": i}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	rows, err := d.Query().
		Where(query.Like("id", "w-finder-%")).
		OrderByDesc("quantity").
		Limit(2).
		Find(ctx)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(rows) != 2 || rows[0].Quantity != 3 || rows[1].Quantity != 2 {
		t.Fatalf("unexpected ordered/limited rows: %+v", rows)
	}
}

func TestFinder_Join_And_Select_WithCategoryName(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))

	catDAO := dao.NewBaseDAO[struct{}](tx, "categories")
	if err := catDAO.Insert(ctx, []string{"id", "name"}, []any{"cat-1", "Category One"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	widgetDAO := dao.NewBaseDAO[testWidget](tx, "widgets")
	if _, err := widgetDAO.InsertMap(ctx, map[string]any{
		"id": "w-join-1", "name": "n", "quantity": 1, "category_id": "cat-1",
	}); err != nil {
		t.Fatalf("seed widget: %v", err)
	}

	joined := dao.NewBaseDAO[testWidgetWithCategory](tx, "widgets")
	row, err := joined.Query().
		Join(query.Join{Type: "LEFT", Table: "categories c", On: "c.id = widgets.category_id"}).
		Select("widgets.id", "widgets.name", "c.name AS category_name").
		Where(query.Eq("widgets.id", "w-join-1")).
		One(ctx)
	if err != nil {
		t.Fatalf("Finder Join+Select One: %v", err)
	}
	if row.CategoryName != "Category One" {
		t.Fatalf("expected joined category_name=%q, got %+v", "Category One", row)
	}
}

func TestFinder_GroupByHaving_AggregatesCorrectly(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))

	catDAO := dao.NewBaseDAO[struct{}](tx, "categories")
	if err := catDAO.Insert(ctx, []string{"id", "name"}, []any{"cat-gb", "Group By Category"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	widgetDAO := dao.NewBaseDAO[testWidget](tx, "widgets")
	for i := 0; i < 3; i++ {
		id := "w-gb-" + string(rune('0'+i))
		if _, err := widgetDAO.InsertMap(ctx, map[string]any{
			"id": id, "name": id, "quantity": 1, "category_id": "cat-gb",
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	agg := dao.NewBaseDAO[categoryCount](tx, "widgets")
	rows, err := agg.Query().
		Where(query.Eq("category_id", "cat-gb")).
		Select("category_id", "COUNT(*) AS total").
		GroupBy("category_id").
		Having(query.Gt("COUNT(*)", 2)).
		Find(ctx)
	if err != nil {
		t.Fatalf("Finder GroupBy+Having Find: %v", err)
	}
	if len(rows) != 1 || rows[0].Count != 3 {
		t.Fatalf("expected one group with count=3, got %+v", rows)
	}
}

func TestFinder_One_Count_Exists_Page_Terminals(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	for i := 0; i < 3; i++ {
		id := "w-term-" + string(rune('0'+i))
		if _, err := d.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": 1}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	cond := query.Like("id", "w-term-%")

	one, err := d.Query().Where(cond).One(ctx)
	if err != nil || one == nil {
		t.Fatalf("One: %+v, err=%v", one, err)
	}
	count, err := d.Query().Where(cond).Count(ctx)
	if err != nil || count != 3 {
		t.Fatalf("Count: %d, err=%v", count, err)
	}
	exists, err := d.Query().Where(cond).Exists(ctx)
	if err != nil || !exists {
		t.Fatalf("Exists: %v, err=%v", exists, err)
	}
	page, err := d.Query().Where(cond).Limit(2).Page(ctx)
	if err != nil || page.Total != 3 || len(page.Data) != 2 {
		t.Fatalf("Page: %+v, err=%v", page, err)
	}
}

func TestBaseDAO_Select_And_SelectOne_RawSQLEscapeHatch(t *testing.T) {
	ctx := context.Background()
	tx := beginTx(t, ctx, testPool(t))
	d := dao.NewBaseDAO[testWidget](tx, "widgets")

	for i := 0; i < 2; i++ {
		id := "w-raw-" + string(rune('0'+i))
		if _, err := d.InsertMap(ctx, map[string]any{"id": id, "name": id, "quantity": i + 1}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	rows, err := d.Select(ctx,
		`SELECT id, name, status, quantity, version, created_at, updated_at
		   FROM widgets
		  WHERE id LIKE 'w-raw-%'
		  ORDER BY quantity DESC`)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(rows) != 2 || rows[0].Quantity < rows[1].Quantity {
		t.Fatalf("unexpected raw-SQL result: %+v", rows)
	}

	one, err := d.SelectOne(ctx,
		`SELECT id, name, status, quantity, version, created_at, updated_at
		   FROM widgets WHERE id = $1`, "w-raw-0")
	if err != nil || one.ID != "w-raw-0" {
		t.Fatalf("SelectOne: %+v, err=%v", one, err)
	}
}
