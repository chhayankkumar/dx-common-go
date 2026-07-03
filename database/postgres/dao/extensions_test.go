package dao

import (
	"context"
	"testing"
)

type row struct {
	ID     string `db:"id"`
	Status string `db:"status"`
}

// TestSoftDeleteScope pins the auto-filter contract: a soft-delete-configured
// DAO prepends the not-deleted predicate; Unscoped removes it without
// mutating the original.
func TestSoftDeleteScope(t *testing.T) {
	d := NewBaseDAOWith[row](nil, "t", WithSoftDelete[row]("status", "DELETED", "ACTIVE"))
	if got := len(d.scope(nil)); got != 1 {
		t.Fatalf("scoped conditions = %d, want 1", got)
	}
	if got := len(d.Unscoped().scope(nil)); got != 0 {
		t.Fatalf("unscoped must add no predicate, got %d", got)
	}
	if got := len(d.scope(nil)); got != 1 {
		t.Fatal("Unscoped must not mutate the original DAO")
	}
	// plain DAO: no predicate
	if got := len(NewBaseDAO[row](nil, "t").scope(nil)); got != 0 {
		t.Fatal("scope on unconfigured DAO must be a no-op")
	}
}

func TestRestoreRequiresConfig(t *testing.T) {
	if err := NewBaseDAO[row](nil, "t").Restore(context.Background(), "x"); err == nil {
		t.Fatal("Restore without WithSoftDelete must error")
	}
}

// TestBulkEmptyInputsNoOp pins that empty inputs never touch the database
// (DB is nil here — any DB call would panic).
func TestBulkEmptyInputsNoOp(t *testing.T) {
	d := NewBaseDAO[row](nil, "t")
	ctx := context.Background()
	if r, err := d.FindByIDs(ctx, nil); err != nil || r != nil {
		t.Fatal("FindByIDs(nil) must no-op")
	}
	if err := d.UpdateByIDs(ctx, nil, map[string]any{"a": 1}); err != nil {
		t.Fatal("UpdateByIDs(nil) must no-op")
	}
	if err := d.DeleteByIDs(ctx, nil); err != nil {
		t.Fatal("DeleteByIDs(nil) must no-op")
	}
	if err := d.InsertBatch(ctx, nil); err != nil {
		t.Fatal("InsertBatch(nil) must no-op")
	}
	if n, err := d.CopyInsert(ctx, []string{"id"}, nil); err != nil || n != 0 {
		t.Fatal("CopyInsert(nil) must no-op")
	}
}
