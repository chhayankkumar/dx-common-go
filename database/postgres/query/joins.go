package query

// Join represents a single JOIN clause.
type Join struct {
	// Type is "INNER", "LEFT", "RIGHT", or "FULL OUTER".
	Type  string
	Table string
	On    string
}
