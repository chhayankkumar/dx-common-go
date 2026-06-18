package pagination

import (
	"testing"
)

func TestRequest_Validate_DefaultValues(t *testing.T) {
	req := Request{Page: 0, PageSize: 0}
	req.Validate()

	if req.Page != 1 {
		t.Fatalf("expected page=1, got %d", req.Page)
	}

	if req.PageSize != 10 {
		t.Fatalf("expected pageSize=10, got %d", req.PageSize)
	}
}

func TestRequest_Validate_LargePageSize(t *testing.T) {
	req := Request{Page: 1, PageSize: 200}
	req.Validate()

	if req.PageSize != 100 {
		t.Fatalf("expected max pageSize=100, got %d", req.PageSize)
	}
}

func TestRequest_Offset(t *testing.T) {
	tests := []struct {
		name     string
		page     int
		pageSize int
		want     int
	}{
		{name: "first page", page: 1, pageSize: 10, want: 0},
		{name: "second page", page: 2, pageSize: 10, want: 10},
		{name: "third page", page: 3, pageSize: 10, want: 20},
		{name: "large page size first", page: 1, pageSize: 100, want: 0},
		{name: "large page size second", page: 2, pageSize: 100, want: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{Page: tt.page, PageSize: tt.pageSize}
			if got := req.Offset(); got != tt.want {
				t.Fatalf("Offset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewResponse(t *testing.T) {
	tests := []struct {
		name           string
		page           int
		pageSize       int
		total          int64
		wantTotalPages int
		wantHasNext    bool
		wantHasPrev    bool
	}{
		{name: "first of many", page: 1, pageSize: 10, total: 250, wantTotalPages: 25, wantHasNext: true, wantHasPrev: false},
		{name: "middle page", page: 5, pageSize: 10, total: 250, wantTotalPages: 25, wantHasNext: true, wantHasPrev: true},
		{name: "last page", page: 25, pageSize: 10, total: 250, wantTotalPages: 25, wantHasNext: false, wantHasPrev: true},
		{name: "single page", page: 1, pageSize: 10, total: 5, wantTotalPages: 1, wantHasNext: false, wantHasPrev: false},
		{name: "zero total", page: 1, pageSize: 10, total: 0, wantTotalPages: 1, wantHasNext: false, wantHasPrev: false},
		{name: "large dataset", page: 50, pageSize: 100, total: 10000, wantTotalPages: 100, wantHasNext: true, wantHasPrev: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{Page: tt.page, PageSize: tt.pageSize}
			resp := NewResponse(req, tt.total)

			if resp.TotalPages != tt.wantTotalPages {
				t.Fatalf("TotalPages = %d, want %d", resp.TotalPages, tt.wantTotalPages)
			}
			if resp.HasNext != tt.wantHasNext {
				t.Fatalf("HasNext = %v, want %v", resp.HasNext, tt.wantHasNext)
			}
			if resp.HasPrev != tt.wantHasPrev {
				t.Fatalf("HasPrev = %v, want %v", resp.HasPrev, tt.wantHasPrev)
			}
		})
	}
}

func TestParsePaginationParams(t *testing.T) {
	tests := []struct {
		name         string
		pageStr      string
		pageSizeStr  string
		wantPage     int
		wantPageSize int
	}{
		{name: "valid values", pageStr: "2", pageSizeStr: "20", wantPage: 2, wantPageSize: 20},
		{name: "invalid strings", pageStr: "invalid", pageSizeStr: "also_invalid", wantPage: 1, wantPageSize: 10},
		{name: "empty strings", pageStr: "", pageSizeStr: "", wantPage: 1, wantPageSize: 10},
		{name: "negative values", pageStr: "-5", pageSizeStr: "-20", wantPage: 1, wantPageSize: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ParsePaginationParams(tt.pageStr, tt.pageSizeStr)
			if req.Page != tt.wantPage {
				t.Fatalf("Page = %d, want %d", req.Page, tt.wantPage)
			}
			if req.PageSize != tt.wantPageSize {
				t.Fatalf("PageSize = %d, want %d", req.PageSize, tt.wantPageSize)
			}
		})
	}
}

func TestNewPaginatedResult(t *testing.T) {
	data := []string{"a", "b", "c"}
	req := Request{Page: 1, PageSize: 10}
	resp := NewResponse(req, 3)

	result := NewPaginatedResult(data, resp)

	if len(result.Data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Data))
	}
	if result.Pagination.Page != 1 {
		t.Fatalf("expected page=1, got %d", result.Pagination.Page)
	}
}
