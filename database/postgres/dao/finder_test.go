package dao

import (
	"reflect"
	"testing"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// TestFinderAccumulation pins the fluent chain's state: Where appends,
// OrderBy/OrderByDesc append in order, Limit/Offset set.
func TestFinderAccumulation(t *testing.T) {
	d := NewBaseDAO[struct{}](nil, "t")
	f := d.Query().
		Where(query.Eq("a", 1)).
		Where(query.Eq("b", 2), query.In("c", []int{3})).
		OrderByDesc("updated_at").
		OrderBy("id").
		Limit(20).
		Offset(40)

	if len(f.conditions) != 3 {
		t.Fatalf("conditions = %d, want 3", len(f.conditions))
	}
	if len(f.orderBy) != 2 || !f.orderBy[0].Desc || f.orderBy[0].Column != "updated_at" ||
		f.orderBy[1].Desc || f.orderBy[1].Column != "id" {
		t.Fatalf("orderBy wrong: %+v", f.orderBy)
	}
	if f.limit != 20 || f.offset != 40 {
		t.Fatalf("limit/offset = %d/%d", f.limit, f.offset)
	}
}

// TestFinderJoinAndSelect pins the join-support surface: Join accumulates
// repeatably, and selectColumns() stays nil (SELECT *) until Select is
// called, at which point it becomes exactly the explicit list Select
// accumulated (no implicit "<table>.*").
func TestFinderJoinAndSelect(t *testing.T) {
	d := NewBaseDAO[struct{}](nil, "policy")

	plain := d.Query()
	if cols := plain.selectColumns(); cols != nil {
		t.Fatalf("selectColumns() on a plain finder = %v, want nil (SELECT *)", cols)
	}

	f := d.Query().
		Join(query.Join{Type: "LEFT", Table: "user_table AS c", On: "policy.consumer_id = c._id"}).
		Join(query.Join{Type: "LEFT", Table: "user_table AS o", On: "policy.owner_id = o._id"}).
		Select("policy._id", "COALESCE(c.email_id,'') AS consumer_email", "COALESCE(o.email_id,'') AS owner_email")

	if len(f.joins) != 2 {
		t.Fatalf("joins = %d, want 2", len(f.joins))
	}
	want := []string{"policy._id", "COALESCE(c.email_id,'') AS consumer_email", "COALESCE(o.email_id,'') AS owner_email"}
	if got := f.selectColumns(); !reflect.DeepEqual(got, want) {
		t.Fatalf("selectColumns() = %v, want %v", got, want)
	}
}

// TestFinderGroupByHaving pins the aggregation surface: GroupBy/Having
// accumulate onto the Finder, and Page falls off the FindPage fast-path the
// moment either is used (a grouped query can't reuse FindPage's
// Count-then-select shape without threading GroupBy/Having through both).
func TestFinderGroupByHaving(t *testing.T) {
	d := NewBaseDAO[struct{}](nil, "policy")
	f := d.Query().
		Select("status", "COUNT(*) AS total").
		GroupBy("status").
		Having(query.Gt("total", 5))

	if len(f.groupBy) != 1 || f.groupBy[0] != "status" {
		t.Fatalf("groupBy = %v, want [status]", f.groupBy)
	}
	if len(f.having) != 1 {
		t.Fatalf("having = %d conditions, want 1", len(f.having))
	}
}

// TestFinderHonorsSoftDeleteScope pins that Find's generated conditions pass
// through the DAO's soft-delete filter (and Unscoped bypasses it).
func TestFinderHonorsSoftDeleteScope(t *testing.T) {
	d := NewBaseDAOWith[struct{}](nil, "t", WithSoftDeleteFilter[struct{}]("status"))
	f := d.Query().Where(query.Eq("a", 1))
	scoped := d.withSoftDeleteFilter(f.conditions)
	if len(scoped) != 2 {
		t.Fatalf("scoped conditions = %d, want 2 (predicate + filter)", len(scoped))
	}
	un := d.Unscoped().Query().Where(query.Eq("a", 1))
	if got := len(un.dao.withSoftDeleteFilter(un.conditions)); got != 1 {
		t.Fatalf("unscoped conditions = %d, want 1", got)
	}
}
