package request

import (
	"net/http/httptest"
	"testing"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

func req(rawQuery string) *Builder {
	r := httptest.NewRequest("GET", "/x?"+rawQuery, nil)
	return From(r)
}

func TestBuild_PaginationDefaults(t *testing.T) {
	pr, err := req("").Build()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if pr.Page != 1 || pr.Size != 10 {
		t.Fatalf("defaults wrong: page=%d size=%d", pr.Page, pr.Size)
	}
	if pr.Offset() != 0 || pr.Limit() != 10 {
		t.Fatalf("offset/limit wrong: %d/%d", pr.Offset(), pr.Limit())
	}
}

func TestBuild_PageSizeOffset(t *testing.T) {
	pr, _ := req("page=3&size=20").Build()
	if pr.Offset() != 40 || pr.Limit() != 20 {
		t.Fatalf("offset=%d limit=%d", pr.Offset(), pr.Limit())
	}
}

func TestBuild_RejectsUnknownParam(t *testing.T) {
	_, err := req("bogus=1").Build()
	if err == nil {
		t.Fatal("expected error for unknown query param")
	}
}

func TestBuild_AllowParams(t *testing.T) {
	// A bespoke param ("choice") is rejected by default but accepted once
	// whitelisted via AllowParams; the handler reads it itself.
	if _, err := req("choice=PENDING").Build(); err == nil {
		t.Fatal("expected unknown 'choice' to be rejected")
	}
	pr, err := req("choice=PENDING&page=2&size=5").AllowParams("choice").Build()
	if err != nil {
		t.Fatalf("AllowParams should accept 'choice': %v", err)
	}
	if pr.Page != 2 || pr.Size != 5 {
		t.Fatalf("pagination wrong: page=%d size=%d", pr.Page, pr.Size)
	}
}

func TestBuild_FilterAllowlistMapping(t *testing.T) {
	pr, err := req("status=ACTIVE&status=CLOSED").
		AllowedFiltersDBMap(map[string]string{"status": "c_status"}).
		Build()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	v, ok := pr.Filters["c_status"]
	if !ok {
		t.Fatalf("expected mapped db column c_status, got %v", pr.Filters)
	}
	if vs, ok := v.([]string); !ok || len(vs) != 2 {
		t.Fatalf("expected 2 values, got %v", v)
	}
}

func TestBuild_SortAllowlistAndMapping(t *testing.T) {
	pr, err := req("sort=createdAt:desc;title:asc").
		AllowedSortFields("createdAt", "title").
		APIToDBMap(map[string]string{"createdAt": "created_at", "title": "title"}).
		Build()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pr.OrderBy) != 2 {
		t.Fatalf("expected 2 order clauses, got %d", len(pr.OrderBy))
	}
	if pr.OrderBy[0].Column != "created_at" || !pr.OrderBy[0].Desc {
		t.Fatalf("first order wrong: %+v", pr.OrderBy[0])
	}
	if pr.OrderBy[1].Column != "title" || pr.OrderBy[1].Desc {
		t.Fatalf("second order wrong: %+v", pr.OrderBy[1])
	}
}

func TestBuild_RejectsDisallowedSortField(t *testing.T) {
	_, err := req("sort=secret:asc").AllowedSortFields("title").Build()
	if err == nil {
		t.Fatal("expected error for disallowed sort field")
	}
}

func TestBuild_DefaultSortApplied(t *testing.T) {
	pr, _ := req("").DefaultSort("created_at", "desc").Build()
	if len(pr.OrderBy) != 1 || pr.OrderBy[0].Column != "created_at" || !pr.OrderBy[0].Desc {
		t.Fatalf("default sort not applied: %+v", pr.OrderBy)
	}
}

func TestConditions_RendersFiltersAndFuzzy(t *testing.T) {
	pr, _ := req("status=ACTIVE&q=climate").
		AllowedFiltersDBMap(map[string]string{"status": "status"}).
		FuzzyFiltersDBMap(map[string]string{"q": "title"}).
		Build()
	conds := pr.Conditions()
	where, args := query.BuildWhere(conds, 1)
	if where == "" || len(args) == 0 {
		t.Fatalf("expected rendered WHERE, got %q args=%v", where, args)
	}
}
