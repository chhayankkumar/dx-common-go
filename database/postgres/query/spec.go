// Specification pattern: package-level Condition constructors and boolean
// combinators, so predicates compose as values —
//
//	query.And(
//	    query.Eq("status", "PENDING"),
//	    query.In("asset_type", types),
//	    query.Between("created_at", from, to),
//	)
//
// — the declarative counterpart of ConditionBuilder's fluent chain. Both
// produce the same []Condition consumed by the SQL builder; use whichever
// reads better at the call site (specs shine when predicates are built up
// across functions or passed around).
package query

// Eq is column = value.
func Eq(column string, value any) Condition { return Condition{Column: column, Op: OpEq, Value: value} }

// NotEq is column <> value.
func NotEq(column string, value any) Condition {
	return Condition{Column: column, Op: OpNotEq, Value: value}
}

// Gt is column > value.
func Gt(column string, value any) Condition { return Condition{Column: column, Op: OpGt, Value: value} }

// Gte is column >= value.
func Gte(column string, value any) Condition {
	return Condition{Column: column, Op: OpGte, Value: value}
}

// Lt is column < value.
func Lt(column string, value any) Condition { return Condition{Column: column, Op: OpLt, Value: value} }

// Lte is column <= value.
func Lte(column string, value any) Condition {
	return Condition{Column: column, Op: OpLte, Value: value}
}

// Like is column LIKE pattern.
func Like(column, pattern string) Condition {
	return Condition{Column: column, Op: OpLike, Value: pattern}
}

// ILike is column ILIKE pattern (case-insensitive).
func ILike(column, pattern string) Condition {
	return Condition{Column: column, Op: OpILike, Value: pattern}
}

// In is column IN (values...). values is a slice.
func In(column string, values any) Condition {
	return Condition{Column: column, Op: OpIn, Value: values}
}

// NotIn is column NOT IN (values...).
func NotIn(column string, values any) Condition {
	return Condition{Column: column, Op: OpNotIn, Value: values}
}

// Between is column BETWEEN low AND high.
func Between(column string, low, high any) Condition {
	return Condition{Column: column, Op: OpBetween, Value: []any{low, high}}
}

// IsNull is column IS NULL.
func IsNull(column string) Condition { return Condition{Column: column, Op: OpIsNull} }

// IsNotNull is column IS NOT NULL.
func IsNotNull(column string) Condition { return Condition{Column: column, Op: OpNotNull} }

// And groups conditions with AND: (c1 AND c2 AND ...).
func And(conditions ...Condition) Condition { return Condition{Op: OpAnd, Sub: conditions} }

// Or groups conditions with OR: (c1 OR c2 OR ...).
func Or(conditions ...Condition) Condition { return Condition{Op: OpOr, Sub: conditions} }
