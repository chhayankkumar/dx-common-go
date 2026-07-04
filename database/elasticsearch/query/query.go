// Package query is the pure request-building DSL of the Elasticsearch
// framework: composable constructors that return JSON-serializable query
// fragments, plus the SearchRequest that assembles them into a request body.
// It performs no I/O and imports no other framework package — services describe
// searches structurally instead of hand-writing JSON, and the repository
// package executes them.
//
//	q := query.Bool().
//	    Must(query.Match("title", "solar pump")).
//	    Filter(query.Term("status", "ACTIVE")).
//	    MustNot(query.Exists("deleted_at")).
//	    Build()
package query

// Query is one Elasticsearch query-DSL fragment.
type Query map[string]any

// MatchAll matches every document.
func MatchAll() Query {
	return Query{"match_all": map[string]any{}}
}

// Match performs full-text matching on one field.
func Match(field string, value any) Query {
	return Query{"match": map[string]any{field: map[string]any{"query": value}}}
}

// MatchFuzzy is Match with a fuzziness setting (e.g. "AUTO", "2").
func MatchFuzzy(field string, value any, fuzziness string) Query {
	return Query{"match": map[string]any{field: map[string]any{"query": value, "fuzziness": fuzziness}}}
}

// MatchPhrase matches the exact phrase.
func MatchPhrase(field string, value any) Query {
	return Query{"match_phrase": map[string]any{field: map[string]any{"query": value}}}
}

// MultiMatch searches value across several fields (supports boosts like "name^3").
func MultiMatch(value any, fields ...string) Query {
	return Query{"multi_match": map[string]any{"query": value, "fields": fields}}
}

// Term matches an exact keyword value.
func Term(field string, value any) Query {
	return Query{"term": map[string]any{field: map[string]any{"value": value}}}
}

// Terms matches any of the exact values.
func Terms[T any](field string, values []T) Query {
	return Query{"terms": map[string]any{field: values}}
}

// Exists matches documents where field has a value.
func Exists(field string) Query {
	return Query{"exists": map[string]any{"field": field}}
}

// Wildcard matches a pattern with * and ? wildcards.
func Wildcard(field, pattern string, caseInsensitive bool) Query {
	body := map[string]any{"value": pattern}
	if caseInsensitive {
		body["case_insensitive"] = true
	}
	return Query{"wildcard": map[string]any{field: body}}
}

// MatchBoolPrefix supports search-as-you-type (autocomplete). The last term
// is treated as a prefix; earlier terms must match fully.
func MatchBoolPrefix(field string, value any) Query {
	return Query{"match_bool_prefix": map[string]any{field: map[string]any{"query": value}}}
}

// Nested queries documents inside a nested object. path is the nested field
// name (e.g. "comments"); inner is the query to run inside that object.
func Nested(path string, inner Query) Query {
	return Query{"nested": map[string]any{"path": path, "query": inner}}
}

// Prefix matches documents where field starts with value. Typically used on
// keyword fields for prefix autocompletion.
func Prefix(field, value string) Query {
	return Query{"prefix": map[string]any{field: map[string]any{"value": value}}}
}

// IDs matches documents by their _id.
func IDs(ids ...string) Query {
	return Query{"ids": map[string]any{"values": ids}}
}

// QueryString runs a Lucene query-string search, optionally limited to fields.
func QueryString(queryStr string, fields ...string) Query {
	body := map[string]any{"query": queryStr}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	return Query{"query_string": body}
}

// Fuzzy matches terms within an edit distance of value. fuzziness is "AUTO",
// "0", "1", or "2" ("AUTO" when empty). Prefer MatchFuzzy for analyzed text;
// Fuzzy operates on exact terms (keyword fields).
func Fuzzy(field string, value any, fuzziness string) Query {
	if fuzziness == "" {
		fuzziness = "AUTO"
	}
	return Query{"fuzzy": map[string]any{field: map[string]any{"value": value, "fuzziness": fuzziness}}}
}

// Regexp matches terms against a regular expression (Lucene syntax). Anchored
// by ES semantics: the pattern must match the whole term. Use sparingly —
// leading wildcards scan the term dictionary.
func Regexp(field, pattern string) Query {
	return Query{"regexp": map[string]any{field: map[string]any{"value": pattern}}}
}

// ScriptQuery filters documents with a Painless predicate (filter context —
// no scoring). params are exposed to the script as params.*.
func ScriptQuery(source string, params map[string]any) Query {
	script := map[string]any{"source": source, "lang": "painless"}
	if len(params) > 0 {
		script["params"] = params
	}
	return Query{"script": map[string]any{"script": script}}
}

// HasChild matches parent documents whose children (declared via a join
// field — see mapping.MappingBuilder.Join) match query. scoreMode is "none",
// "avg", "sum", "max", or "min" ("none" when empty).
func HasChild(childType string, query Query, scoreMode string) Query {
	body := map[string]any{"type": childType, "query": query}
	if scoreMode != "" {
		body["score_mode"] = scoreMode
	}
	return Query{"has_child": body}
}

// HasParent matches child documents whose parent matches query.
func HasParent(parentType string, query Query) Query {
	return Query{"has_parent": map[string]any{"parent_type": parentType, "query": query}}
}

// ParentID matches children of one specific parent document.
func ParentID(childType, id string) Query {
	return Query{"parent_id": map[string]any{"type": childType, "id": id}}
}
