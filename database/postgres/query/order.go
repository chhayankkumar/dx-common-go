package query

import "strings"

// OrderBy specifies a column sort direction.
type OrderBy struct {
	Column string
	Desc   bool
}

// BuildOrderBy renders an ORDER BY clause (including the "ORDER BY" keyword)
// from the supplied OrderBy slice, or "" when empty.
//
// Security: OrderBy.Column is emitted verbatim — supply only allowlist-mapped
// column names (see request.PaginationRequestBuilder, which maps user sort keys
// through an apiToDb allowlist before producing OrderBy values).
func BuildOrderBy(orders []OrderBy) string {
	if len(orders) == 0 {
		return ""
	}
	parts := make([]string, 0, len(orders))
	for _, o := range orders {
		dir := "ASC"
		if o.Desc {
			dir = "DESC"
		}
		parts = append(parts, o.Column+" "+dir)
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}
