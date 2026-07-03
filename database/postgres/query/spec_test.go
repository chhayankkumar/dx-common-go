package query

import (
	"strings"
	"testing"
)

// TestSpecEquivalence pins that package-level constructors produce the same
// conditions as the fluent builder.
func TestSpecEquivalence(t *testing.T) {
	spec := []Condition{Eq("a", 1), Gte("b", 2), In("c", []string{"x"})}
	built := NewConditionBuilder().Eq("a", 1).Gte("b", 2).In("c", []string{"x"}).Build()
	if len(spec) != len(built) {
		t.Fatalf("len mismatch %d vs %d", len(spec), len(built))
	}
	for i := range spec {
		if spec[i].Column != built[i].Column || spec[i].Op != built[i].Op {
			t.Fatalf("condition %d mismatch: %+v vs %+v", i, spec[i], built[i])
		}
	}
}

// TestSpecNestedRendering pins that And/Or combinators render as bracketed
// groups with correctly numbered placeholders.
func TestSpecNestedRendering(t *testing.T) {
	q := SelectQuery{
		Table: "t",
		Conditions: []Condition{
			Or(
				Eq("status", "PENDING"),
				And(Eq("status", "GRANTED"), Gte("expiry_at", "now")),
			),
		},
	}
	sql, args := New().BuildSelect(q)
	if !strings.Contains(sql, "(status = $1 OR (status = $2 AND expiry_at >= $3))") {
		t.Fatalf("nested group rendering wrong: %s", sql)
	}
	if len(args) != 3 {
		t.Fatalf("args = %d, want 3", len(args))
	}
}

// TestBetweenSpecMatchesBuilder pins the []any{low,high} value shape.
func TestBetweenSpecMatchesBuilder(t *testing.T) {
	sql, args := New().BuildSelect(SelectQuery{
		Table:      "t",
		Conditions: []Condition{Between("created_at", 1, 2)},
	})
	if !strings.Contains(sql, "BETWEEN $1 AND $2") || len(args) != 2 {
		t.Fatalf("between rendering wrong: %s (%d args)", sql, len(args))
	}
}
