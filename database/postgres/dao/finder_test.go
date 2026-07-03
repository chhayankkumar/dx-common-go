package dao

import (
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
