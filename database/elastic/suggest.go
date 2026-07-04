package elastic

import "encoding/json"

// Suggester is one named entry in a search request's "suggest" block. Build
// with TermSuggester, PhraseSuggester, or CompletionSuggester.
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
// (see MappingBuilder.Completion). fuzzy enables typo tolerance.
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

// SuggestOption is one proposed suggestion.
type SuggestOption struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	// Source carries the suggesting document for completion suggesters
	// (absent for term/phrase suggesters).
	Source json.RawMessage `json:"_source,omitempty"`
}

// suggestEntry mirrors ES's per-input-token response shape.
type suggestEntry struct {
	Text    string          `json:"text"`
	Options []SuggestOption `json:"options"`
}

// flattenSuggest folds the raw suggest response into name → options.
func flattenSuggest(raw map[string][]suggestEntry) map[string][]SuggestOption {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]SuggestOption, len(raw))
	for name, entries := range raw {
		var opts []SuggestOption
		for _, e := range entries {
			opts = append(opts, e.Options...)
		}
		out[name] = opts
	}
	return out
}
