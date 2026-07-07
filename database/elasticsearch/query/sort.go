package query

// Sort is one ordered list of sort keys, applied in order. Each entry maps a
// field to "asc" or "desc". A SearchRequest takes []map[string]string directly;
// these helpers make building one readable.
type Sort []map[string]string

// By starts a sort ordering with one key.
func By(field, direction string) Sort {
	return Sort{{field: direction}}
}

// Asc appends an ascending key.
func (s Sort) Asc(field string) Sort { return append(s, map[string]string{field: "asc"}) }

// Desc appends a descending key.
func (s Sort) Desc(field string) Sort { return append(s, map[string]string{field: "desc"}) }
