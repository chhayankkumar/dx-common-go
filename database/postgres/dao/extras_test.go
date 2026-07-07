package dao

import (
	"context"
	"testing"
)

type auditRow struct {
	ID     string `db:"id"`
	Status string `db:"status"`
}

func TestActorContext(t *testing.T) {
	if _, ok := ActorFromContext(context.Background()); ok {
		t.Fatal("empty context must have no actor")
	}
	ctx := WithActor(context.Background(), "user-1")
	got, ok := ActorFromContext(ctx)
	if !ok || got != "user-1" {
		t.Fatalf("actor = %q ok=%v", got, ok)
	}
}

// TestAuditInsertUpdate pins the copy-on-write auto-population rules.
func TestAuditInsertUpdate(t *testing.T) {
	d := NewBaseDAOWith[auditRow](nil, "t", WithAuditColumns[auditRow]("created_by", "updated_by"))
	ctx := WithActor(context.Background(), "user-1")

	in := map[string]any{"name": "x"}
	out := d.auditInsert(ctx, in)
	if out["created_by"] != "user-1" || out["updated_by"] != "user-1" {
		t.Fatalf("audit insert not populated: %v", out)
	}
	if _, mutated := in["created_by"]; mutated {
		t.Fatal("caller map must not be mutated")
	}

	// caller-set value wins
	explicit := d.auditInsert(ctx, map[string]any{"created_by": "system"})
	if explicit["created_by"] != "system" {
		t.Fatal("explicit created_by must not be overridden")
	}

	// update only sets updated_by
	set := d.auditUpdate(ctx, map[string]any{"status": "ACTIVE"})
	if set["updated_by"] != "user-1" {
		t.Fatalf("audit update not populated: %v", set)
	}
	if _, has := set["created_by"]; has {
		t.Fatal("update must not set created_by")
	}

	// no actor → untouched (same map back)
	same := d.auditInsert(context.Background(), in)
	if len(same) != len(in) {
		t.Fatal("no-actor insert must be a no-op")
	}

	// audit off → untouched
	plain := NewBaseDAO[auditRow](nil, "t")
	if got := plain.auditInsert(ctx, in); len(got) != len(in) {
		t.Fatal("unconfigured DAO must not audit")
	}
}

func TestAuditUpdateColumns(t *testing.T) {
	d := NewBaseDAOWith[auditRow](nil, "t", WithAuditColumns[auditRow]("", "updated_by"))
	got := d.auditUpdateColumns([]string{"status"})
	if len(got) != 2 || got[1] != "updated_by" {
		t.Fatalf("updated_by not appended: %v", got)
	}
	// idempotent
	if again := d.auditUpdateColumns(got); len(again) != 2 {
		t.Fatalf("must not duplicate: %v", again)
	}
}

func TestSoftDeleteValues(t *testing.T) {
	d := NewBaseDAOWith[auditRow](nil, "t",
		WithSoftDeleteFilter[auditRow]("state"),
		WithSoftDeleteValues[auditRow]("GONE", "LIVE"))
	if d.deletedValue() != "GONE" || d.activeValue() != "LIVE" {
		t.Fatalf("sentinels = %q/%q", d.deletedValue(), d.activeValue())
	}
	conds := d.withSoftDeleteFilter(nil)
	if len(conds) != 1 || conds[0].Value != "GONE" {
		t.Fatalf("filter must use custom sentinel: %+v", conds)
	}
	// defaults
	def := NewBaseDAOWith[auditRow](nil, "t", WithSoftDeleteFilter[auditRow]("status"))
	if def.deletedValue() != "DELETED" || def.activeValue() != "ACTIVE" {
		t.Fatal("default sentinels must be DELETED/ACTIVE")
	}
}

func TestRestoreRequiresSoftDeleteConfig(t *testing.T) {
	if err := NewBaseDAO[auditRow](nil, "t").Restore(context.Background(), "x"); err == nil {
		t.Fatal("Restore without WithSoftDeleteFilter must error")
	}
}

// TestByIDsEmptyNoOp pins that empty id sets never touch the database
// (DB is nil — any call would panic).
func TestByIDsEmptyNoOp(t *testing.T) {
	d := NewBaseDAO[auditRow](nil, "t")
	ctx := context.Background()
	if rows, err := d.FindByIDs(ctx, nil); err != nil || rows != nil {
		t.Fatal("FindByIDs(nil) must no-op")
	}
	if err := d.UpdateByIDs(ctx, nil, map[string]any{"a": 1}); err != nil {
		t.Fatal("UpdateByIDs(nil) must no-op")
	}
	if err := d.DeleteByIDs(ctx, nil); err != nil {
		t.Fatal("DeleteByIDs(nil) must no-op")
	}
}
