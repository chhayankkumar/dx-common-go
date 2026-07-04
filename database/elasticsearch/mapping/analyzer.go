package mapping

// CustomAnalyzer registers an analyzer built from a tokenizer + filter chain,
// e.g. CustomAnalyzer("en_text", "standard", "lowercase", "porter_stem").
func (m *MappingBuilder) CustomAnalyzer(name, tokenizer string, filters ...string) *MappingBuilder {
	if m.analyzers == nil {
		m.analyzers = map[string]any{}
	}
	m.analyzers[name] = map[string]any{"type": "custom", "tokenizer": tokenizer, "filter": filters}
	return m
}

// Tokenizer registers a custom tokenizer definition, e.g.
// Tokenizer("edge_2_20", map[string]any{"type": "edge_ngram", "min_gram": 2, "max_gram": 20}).
func (m *MappingBuilder) Tokenizer(name string, def map[string]any) *MappingBuilder {
	if m.tokenizers == nil {
		m.tokenizers = map[string]any{}
	}
	m.tokenizers[name] = def
	return m
}

// TokenFilter registers a custom token filter definition.
func (m *MappingBuilder) TokenFilter(name string, def map[string]any) *MappingBuilder {
	if m.filters == nil {
		m.filters = map[string]any{}
	}
	m.filters[name] = def
	return m
}

// Synonyms registers a synonym filter (wire it into a CustomAnalyzer's
// chain). Entries use Solr syntax: "car, automobile" or "tv => television".
func (m *MappingBuilder) Synonyms(name string, entries ...string) *MappingBuilder {
	return m.TokenFilter(name, map[string]any{"type": "synonym_graph", "synonyms": entries})
}

// Normalizer registers a keyword normalizer (case-insensitive sort/agg on
// keyword fields), e.g. Normalizer("lowercase_sort", "lowercase").
func (m *MappingBuilder) Normalizer(name string, filters ...string) *MappingBuilder {
	if m.normalizer == nil {
		m.normalizer = map[string]any{}
	}
	m.normalizer[name] = map[string]any{"type": "custom", "filter": filters}
	return m
}

// Setting adds a raw index-level setting, e.g. Setting("number_of_shards", 1)
// or Setting("refresh_interval", "30s").
func (m *MappingBuilder) Setting(key string, value any) *MappingBuilder {
	if m.settings == nil {
		m.settings = map[string]any{}
	}
	m.settings[key] = value
	return m
}

// Shards sets number_of_shards + number_of_replicas together.
func (m *MappingBuilder) Shards(primaries, replicas int) *MappingBuilder {
	return m.Setting("number_of_shards", primaries).Setting("number_of_replicas", replicas)
}
