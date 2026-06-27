package model

import (
	"net/http"
	"strconv"
)

const (
	defaultLimit = 10
	maxLimit     = 100
	defaultOffset = 0
)

// PaginationRequest carries validated limit/offset query parameters.
//
// Deprecated: prefer request.PaginatedRequest (page/size + allowlist sort/filter)
// via request.From(r).Build(). It is the single canonical paginate/sort/filter
// entry point for all dx services; this limit/offset form is retained only for
// endpoints not yet migrated.
type PaginationRequest struct {
	Limit  int `json:"limit"  validate:"min=1,max=100"`
	Offset int `json:"offset" validate:"min=0"`
}

// ParsePagination reads "limit" and "offset" from r's query string and returns
// a PaginationRequest with defaults applied. Invalid or out-of-range values are
// silently clamped to defaults.
//
// Deprecated: use request.From(r).Build() (page/size, allowlist-mapped sort and
// filters) — the canonical dx paginate/sort/filter module.
func ParsePagination(r *http.Request) PaginationRequest {
	q := r.URL.Query()

	limit := defaultLimit
	if l := q.Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v >= 1 {
			limit = v
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset := defaultOffset
	if o := q.Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	return PaginationRequest{Limit: limit, Offset: offset}
}
