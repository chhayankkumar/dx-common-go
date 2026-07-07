package query

// KNN is one approximate nearest-neighbour clause (dense_vector fields).
// Combined with a SearchRequest.Query, Elasticsearch blends both result sets —
// the hybrid-search form. Requires a dense_vector mapping with index: true.
type KNN struct {
	// Field is the dense_vector field to search.
	Field string
	// QueryVector is the embedding to match against.
	QueryVector []float32
	// K is the number of neighbours to return.
	K int
	// NumCandidates is the per-shard candidate pool (>= K; higher = better
	// recall, slower). Defaults to 10*K when zero.
	NumCandidates int
	// Filter restricts the candidate documents (applied during the ANN walk).
	Filter Query
	// Boost weights this clause when blending with Query (hybrid search).
	Boost float64
}

// Body renders the knn clause.
func (k KNN) Body() map[string]any {
	numCandidates := k.NumCandidates
	if numCandidates <= 0 {
		numCandidates = 10 * k.K
	}
	body := map[string]any{
		"field":          k.Field,
		"query_vector":   k.QueryVector,
		"k":              k.K,
		"num_candidates": numCandidates,
	}
	if k.Filter != nil {
		body["filter"] = k.Filter
	}
	if k.Boost > 0 {
		body["boost"] = k.Boost
	}
	return body
}

// PIT references an open point-in-time (repository.OpenPIT). Pinning a search
// to a PIT gives consistent deep pagination with Sort + SearchAfter.
type PIT struct {
	ID string
	// KeepAlive extends the PIT on each use, e.g. "1m".
	KeepAlive string
}
