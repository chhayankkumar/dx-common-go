package query

// Suggester is one named entry in a search request's "suggest" block. Build
// with TermSuggester, PhraseSuggester, or CompletionSuggester. (Suggestion
// results arrive on the response side — see repository.SuggestOption.)
type Suggester map[string]any

// TermSuggester proposes spelling corrections for text, term by term.
func TermSuggester(text, field string) Suggester {
	return Suggester{"text": text, "term": map[string]any{"field": field}}
}

// PhraseSuggester proposes whole-phrase corrections ("did you mean …").
func PhraseSuggester(text, field string) Suggester {
	return Suggester{"text": text, "phrase": map[string]any{"field": field}}
}

// CompletionSuggester serves type-ahead from a mapping "completion" field
// (see mapping.MappingBuilder.Completion). fuzzy enables typo tolerance.
func CompletionSuggester(prefix, field string, size int, fuzzy bool) Suggester {
	completion := map[string]any{"field": field}
	if size > 0 {
		completion["size"] = size
	}
	if fuzzy {
		completion["fuzzy"] = map[string]any{"fuzziness": "AUTO"}
	}
	return Suggester{"prefix": prefix, "completion": completion}
}
