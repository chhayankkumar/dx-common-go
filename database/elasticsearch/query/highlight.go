package query

// Highlight describes a highlighting request. Zero values fall back to
// Elasticsearch defaults (<em> tags, 100-char fragments, 5 fragments).
type Highlight struct {
	// Fields to highlight (required).
	Fields []string
	// PreTags/PostTags wrap each match, e.g. ["<mark>"] / ["</mark>"].
	PreTags  []string
	PostTags []string
	// FragmentSize is the fragment length in characters.
	FragmentSize int
	// NumberOfFragments caps fragments per field; 0 keeps the ES default.
	NumberOfFragments int
}

// Body renders the highlight block for a search request.
func (h *Highlight) Body() map[string]any {
	fields := make(map[string]any, len(h.Fields))
	for _, f := range h.Fields {
		fields[f] = map[string]any{}
	}
	body := map[string]any{"fields": fields}
	if len(h.PreTags) > 0 {
		body["pre_tags"] = h.PreTags
	}
	if len(h.PostTags) > 0 {
		body["post_tags"] = h.PostTags
	}
	if h.FragmentSize > 0 {
		body["fragment_size"] = h.FragmentSize
	}
	if h.NumberOfFragments > 0 {
		body["number_of_fragments"] = h.NumberOfFragments
	}
	return body
}
