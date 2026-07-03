package dao

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeQuerier captures the SQL/args it receives instead of hitting a real
// database — enough to assert on statement shape for logic that doesn't
// need real row data (soft-delete filtering, InsertMany, UpdateVersioned).
type fakeQuerier struct {
	lastSQL  string
	lastArgs []any
}

func (f *fakeQuerier) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.lastSQL, f.lastArgs = sql, args
	return nil, errors.New("fakeQuerier: no rows backing this test double")
}

func (f *fakeQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.lastSQL, f.lastArgs = sql, args
	return fakeRow{}
}

func (f *fakeQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.lastSQL, f.lastArgs = sql, args
	return pgconn.CommandTag{}, nil
}

type fakeRow struct{}

func (fakeRow) Scan(...any) error { return errors.New("fakeRow: no data backing this test double") }

type widget struct {
	ID   string
	Name string
}

func TestWithSoftDeleteFilter_AppliedToFindAll(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAOWith[widget](q, "widgets", WithSoftDeleteFilter[widget]("status"))

	_, _ = d.FindAll(context.Background(), nil)

	want := "SELECT * FROM widgets WHERE status <> $1"
	if q.lastSQL != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", q.lastSQL, want)
	}
	if len(q.lastArgs) != 1 || q.lastArgs[0] != "DELETED" {
		t.Fatalf("expected args [DELETED], got %v", q.lastArgs)
	}
}

func TestUnscoped_SuspendsSoftDeleteFilter(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAOWith[widget](q, "widgets", WithSoftDeleteFilter[widget]("status"))

	_, _ = d.Unscoped().FindAll(context.Background(), nil)

	want := "SELECT * FROM widgets"
	if q.lastSQL != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", q.lastSQL, want)
	}

	// The original (non-Unscoped) DAO must be unaffected by the clone.
	_, _ = d.FindAll(context.Background(), nil)
	if q.lastSQL != "SELECT * FROM widgets WHERE status <> $1" {
		t.Fatalf("Unscoped() leaked into the receiver DAO: %s", q.lastSQL)
	}
}

func TestWithoutSoftDeleteFilter_Unaffected(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	_, _ = d.FindAll(context.Background(), nil)

	if q.lastSQL != "SELECT * FROM widgets" {
		t.Fatalf("DAO without WithSoftDeleteFilter should be unaffected, got: %s", q.lastSQL)
	}
}

func TestInsertMany_RowLengthMismatch(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	err := d.InsertMany(context.Background(), []string{"id", "name"}, [][]any{{"w1", "one"}, {"w2"}})
	if err == nil {
		t.Fatal("expected an error for a short row, got nil")
	}
	if q.lastSQL != "" {
		t.Fatalf("Exec should not have been called before validation failed, got SQL: %s", q.lastSQL)
	}
}

func TestInsertMany_BuildsMultiValuesStatement(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	if err := d.InsertMany(context.Background(), []string{"id", "name"}, [][]any{{"w1", "one"}, {"w2", "two"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "INSERT INTO widgets (id, name) VALUES ($1,$2), ($3,$4)"
	if q.lastSQL != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", q.lastSQL, want)
	}
	if len(q.lastArgs) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(q.lastArgs), q.lastArgs)
	}
}

func TestInsertIgnore_BuildsOnConflictDoNothing(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	if _, err := d.InsertIgnore(context.Background(), []string{"id", "name"}, []any{"w1", "one"}, "id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "INSERT INTO widgets (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING"
	if q.lastSQL != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", q.lastSQL, want)
	}
}

func TestInsertIgnore_ColumnValueMismatch(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	if _, err := d.InsertIgnore(context.Background(), []string{"id", "name"}, []any{"w1"}, "id"); err == nil {
		t.Fatal("expected an error for mismatched columns/values, got nil")
	}
	if q.lastSQL != "" {
		t.Fatalf("Exec should not have been called before validation failed, got SQL: %s", q.lastSQL)
	}
}

func TestCopyFrom_UnsupportedQuerier(t *testing.T) {
	q := &fakeQuerier{} // fakeQuerier does not implement the copier interface
	d := NewBaseDAO[widget](q, "widgets")

	if _, err := d.CopyFrom(context.Background(), []string{"id", "name"}, [][]any{{"w1", "one"}}); err == nil {
		t.Fatal("expected an error when the underlying Querier does not support CopyFrom")
	}
}

func TestUpdateVersioned_BuildsIncrementAndVersionGuard(t *testing.T) {
	q := &fakeQuerier{}
	d := NewBaseDAO[widget](q, "widgets")

	_, _ = d.UpdateVersioned(context.Background(), map[string]any{"name": "renamed"}, nil, "version", 5)

	want := "UPDATE widgets SET name = $1, version = version + 1 WHERE version = $2 RETURNING *"
	if q.lastSQL != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", q.lastSQL, want)
	}
	if len(q.lastArgs) != 2 || q.lastArgs[1] != int64(5) {
		t.Fatalf("expected args [renamed, 5], got %v", q.lastArgs)
	}
}
