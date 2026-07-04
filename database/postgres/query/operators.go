package query

// Operator represents a SQL comparison or membership operator.
type Operator string

const (
	OpEq      Operator = "="
	OpNotEq   Operator = "<>"
	OpGt      Operator = ">"
	OpGte     Operator = ">="
	OpLt      Operator = "<"
	OpLte     Operator = "<="
	OpLike    Operator = "LIKE"
	OpILike   Operator = "ILIKE"
	OpIn      Operator = "IN"
	OpNotIn   Operator = "NOT IN"
	OpBetween Operator = "BETWEEN"
	OpIsNull  Operator = "IS NULL"
	OpNotNull Operator = "IS NOT NULL"
	OpAnd     Operator = "AND"
	OpOr      Operator = "OR"
)
