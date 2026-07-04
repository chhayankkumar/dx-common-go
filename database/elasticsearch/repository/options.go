package repository

// SearchOpts carries the non-query parts of a Repo.Search call.
type SearchOpts struct {
	Size           int
	From           int
	Sort           []map[string]string
	SourceIncludes []string
	SourceExcludes []string
	TrackTotalHits bool
}
