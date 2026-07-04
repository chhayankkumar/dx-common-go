package query

// SelectQuery describes a SELECT statement.
type SelectQuery struct {
	Table      string
	Columns    []string // empty means "*"
	Joins      []Join
	Conditions []Condition
	// GroupBy lists columns/expressions for a GROUP BY clause, emitted
	// verbatim (same trust boundary as OrderBy.Column) — combine with
	// Columns to select the grouped columns plus aggregate expressions
	// (e.g. "COUNT(*) AS total"). Must include every non-aggregated column
	// named in Columns (ordinary SQL rule, not enforced here).
	GroupBy []string
	// Having lists post-aggregation filter predicates, rendered after GROUP
	// BY using the same Condition model Conditions uses for WHERE.
	Having    []Condition
	OrderBy   []OrderBy
	Limit     int
	Offset    int
	ForUpdate bool
}

// InsertQuery describes an INSERT statement.
type InsertQuery struct {
	Table     string
	Columns   []string
	Values    []any
	Returning []string
}

// UpdateQuery describes an UPDATE statement.
type UpdateQuery struct {
	Table string
	Set   map[string]any
	// Increment lists columns to bump by 1 (col = col + 1) alongside Set —
	// for counters and optimistic-locking version columns, which can't be
	// expressed as a literal Set value.
	Increment  []string
	Conditions []Condition
	Returning  []string
}

// DeleteQuery describes a DELETE (or soft-delete UPDATE) statement.
type DeleteQuery struct {
	Table      string
	Conditions []Condition
	// SoftDelete, when true, generates an UPDATE SET status='DELETED' instead.
	SoftDelete bool
}

// UpsertQuery describes an INSERT … ON CONFLICT DO UPDATE statement.
type UpsertQuery struct {
	Table          string
	Columns        []string
	Values         []any
	ConflictColumn string
	UpdateColumns  []string
	Returning      []string
}
