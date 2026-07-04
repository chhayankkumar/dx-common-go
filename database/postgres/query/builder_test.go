package query

import (
	"reflect"
	"testing"
)

func TestBuildSelect_InAndBetween(t *testing.T) {
	conds := NewConditionBuilder().
		Eq("status", "ACTIVE").
		In("category", []string{"A", "B"}).
		Between("created_at", "2026-01-01", "2026-02-01").
		Build()

	sql, args := New().BuildSelect(SelectQuery{
		Table:      "items",
		Conditions: conds,
		OrderBy:    []OrderBy{{Column: "created_at", Desc: true}},
		Limit:      10,
		Offset:     20,
	})

	want := "SELECT * FROM items WHERE status = $1 AND category = ANY($2) AND created_at BETWEEN $3 AND $4 ORDER BY created_at DESC LIMIT $5 OFFSET $6"
	if sql != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", sql, want)
	}
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}
}

func TestBuildSelect_Joins(t *testing.T) {
	sql, args := New().BuildSelect(SelectQuery{
		Table:   "policy",
		Columns: []string{"policy.*", "COALESCE(c.email_id,'') AS consumer_email"},
		Joins: []Join{
			{Type: "LEFT", Table: "user_table AS c", On: "policy.consumer_id = c._id"},
			{Type: "LEFT", Table: "user_table AS o", On: "policy.owner_id = o._id"},
		},
		Conditions: NewConditionBuilder().Eq("policy._id", "id-1").Build(),
	})

	want := "SELECT policy.*, COALESCE(c.email_id,'') AS consumer_email FROM policy" +
		" LEFT JOIN user_table AS c ON policy.consumer_id = c._id" +
		" LEFT JOIN user_table AS o ON policy.owner_id = o._id" +
		" WHERE policy._id = $1"
	if sql != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", sql, want)
	}
	if len(args) != 1 || args[0] != "id-1" {
		t.Fatalf("args = %v, want [id-1]", args)
	}
}

func TestBuildSelect_GroupByHaving(t *testing.T) {
	sql, args := New().BuildSelect(SelectQuery{
		Table:      "policy",
		Columns:    []string{"status", "COUNT(*) AS total"},
		Conditions: NewConditionBuilder().Eq("item_organization_id", "org-1").Build(),
		GroupBy:    []string{"status"},
		Having:     []Condition{Gt("total", 5)},
	})

	want := "SELECT status, COUNT(*) AS total FROM policy WHERE item_organization_id = $1" +
		" GROUP BY status HAVING total > $2"
	if sql != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", sql, want)
	}
	if len(args) != 2 || args[0] != "org-1" || args[1] != 5 {
		t.Fatalf("args = %v, want [org-1 5]", args)
	}
}

func TestBuildSelect_NotIn(t *testing.T) {
	sql, _ := New().BuildSelect(SelectQuery{
		Table:      "t",
		Conditions: NewConditionBuilder().NotIn("status", []string{"DELETED"}).Build(),
	})
	want := "SELECT * FROM t WHERE status <> ALL($1)"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestFromFilters(t *testing.T) {
	conds := FromFilters(map[string]any{
		"subject_id":   "u1",
		"empty":        "",
		"nilval":       nil,
		"resource_ids": []string{"r1", "r2"},
		"single":       []string{"only"},
	})

	// sorted key order: resource_ids, single, subject_id
	if len(conds) != 3 {
		t.Fatalf("expected 3 conditions, got %d: %+v", len(conds), conds)
	}
	if conds[0].Column != "resource_ids" || conds[0].Op != OpIn {
		t.Fatalf("expected resource_ids IN first, got %+v", conds[0])
	}
	if conds[1].Column != "single" || conds[1].Op != OpEq || conds[1].Value != "only" {
		t.Fatalf("single-element slice should collapse to Eq, got %+v", conds[1])
	}
	if conds[2].Column != "subject_id" || conds[2].Op != OpEq {
		t.Fatalf("expected subject_id Eq, got %+v", conds[2])
	}
}

func TestFromTemporal(t *testing.T) {
	conds := FromTemporal([]TemporalFilter{
		{Field: "created_at", Rel: "between", Time: "a", End: "b"},
		{Field: "expires_at", Rel: "after", Time: "c"},
	})
	if len(conds) != 2 || conds[0].Op != OpBetween || conds[1].Op != OpGt {
		t.Fatalf("unexpected temporal conditions: %+v", conds)
	}
	if !reflect.DeepEqual(conds[0].Value, []any{"a", "b"}) {
		t.Fatalf("between values wrong: %+v", conds[0].Value)
	}
}

func TestBuildUpdate_Increment(t *testing.T) {
	sql, args := New().BuildUpdate(UpdateQuery{
		Table:      "widgets",
		Set:        map[string]any{"name": "new-name"},
		Increment:  []string{"version"},
		Conditions: NewConditionBuilder().Eq("id", "w1").Eq("version", int64(3)).Build(),
		Returning:  []string{"*"},
	})
	want := "UPDATE widgets SET name = $1, version = version + 1 WHERE id = $2 AND version = $3 RETURNING *"
	if sql != want {
		t.Fatalf("sql mismatch:\n got: %s\nwant: %s", sql, want)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
}

func TestBuildUpsert(t *testing.T) {
	sql, args := New().BuildUpsert(UpsertQuery{
		Table:          "kv",
		Columns:        []string{"k", "v"},
		Values:         []any{"a", "b"},
		ConflictColumn: "k",
		UpdateColumns:  []string{"v"},
		Returning:      []string{"*"},
	})
	want := "INSERT INTO kv (k, v) VALUES ($1, $2) ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v RETURNING *"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
}
