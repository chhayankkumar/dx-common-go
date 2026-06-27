package query

import "strings"

// BuildWhere renders a slice of Conditions into a WHERE body (WITHOUT the
// leading "WHERE" keyword), binding values as $N placeholders starting at
// startIdx. It returns the SQL fragment and the ordered argument slice.
//
// This lets services splice allowlist-built conditions (e.g. from
// request.PaginatedRequest + FromFilters) into hand-written queries — CTEs,
// computed columns, joins — that the full SQLBuilder cannot express. Pass the
// next free placeholder index as startIdx (1 if the fragment is the first set
// of parameters in the statement).
//
// Security: Column identifiers are emitted verbatim; only values are bound.
// Callers MUST ensure Condition.Column values come from trusted sources
// (allowlists), never raw user input.
func BuildWhere(conditions []Condition, startIdx int) (string, []any) {
	if len(conditions) == 0 {
		return "", nil
	}
	var args []any
	idx := startIdx
	sql := buildConditions(conditions, &args, &idx)
	return sql, args
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

// JoinAnd combines an existing WHERE fragment with additional rendered
// conditions using AND, returning a single fragment. Either side may be empty.
func JoinAnd(fragments ...string) string {
	nonEmpty := make([]string, 0, len(fragments))
	for _, f := range fragments {
		if strings.TrimSpace(f) != "" {
			nonEmpty = append(nonEmpty, f)
		}
	}
	return strings.Join(nonEmpty, " AND ")
}
