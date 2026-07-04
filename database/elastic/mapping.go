package elastic

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// MappingBuilder assembles an index-creation body — mappings (field types,
// multi-fields, nested/join/vector fields, dynamic templates, runtime
// fields) plus analysis settings (analyzers, tokenizers, filters, synonyms)
// — for CreateIndex / EnsureIndex / MigrateIndex.
//
// Version mappings the same way as SQL migrations: the builder call lives in
// the owning service, the physical index is named <alias>-vN, and a mapping
// change that isn't expand-only ships as a new version + Reindex + SwapAlias
// (DATABASE.md §8.4). Mappings are code — reviewed, diffed, and testable.
type MappingBuilder struct {
	properties       map[string]any
	dynamic          string
	dynamicTemplates []map[string]any
	runtime          map[string]any

	analyzers  map[string]any
	tokenizers map[string]any
	filters    map[string]any
	normalizer map[string]any
	settings   map[string]any
}

// NewMapping starts a mapping.
func NewMapping() *MappingBuilder {
	return &MappingBuilder{properties: map[string]any{}}
}

// Dynamic sets the mapping's dynamic mode: "true", "false", or "strict".
// Production indices should usually be "strict" or "false" — unmapped fields
// appearing at index time are a schema change nobody reviewed.
func (m *MappingBuilder) Dynamic(mode string) *MappingBuilder { m.dynamic = mode; return m }

// Field adds a field with an explicit property body — the escape hatch for
// anything the typed helpers below don't cover.
func (m *MappingBuilder) Field(name string, property map[string]any) *MappingBuilder {
	m.properties[name] = property
	return m
}

// Text adds an analyzed full-text field. analyzer is optional ("" = standard).
func (m *MappingBuilder) Text(name, analyzer string) *MappingBuilder {
	p := map[string]any{"type": "text"}
	if analyzer != "" {
		p["analyzer"] = analyzer
	}
	return m.Field(name, p)
}

// TextWithKeyword adds the standard text + .keyword multi-field: full-text
// search on the field, exact filter/sort/agg on <name>.keyword.
func (m *MappingBuilder) TextWithKeyword(name string) *MappingBuilder {
	return m.Field(name, map[string]any{
		"type": "text",
		"fields": map[string]any{
			"keyword": map[string]any{"type": "keyword", "ignore_above": 256},
		},
	})
}

// Keyword adds an exact-value field (filters, sorts, aggregations).
func (m *MappingBuilder) Keyword(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "keyword"})
}

// Date, Long, Double, Boolean add the corresponding scalar fields.
func (m *MappingBuilder) Date(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "date"})
}
func (m *MappingBuilder) Long(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "long"})
}
func (m *MappingBuilder) Double(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "double"})
}
func (m *MappingBuilder) Boolean(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "boolean"})
}

// GeoPoint / GeoShape add geo fields (GeoDistance / GeoBoundingBox /
// GeoShape queries).
func (m *MappingBuilder) GeoPoint(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "geo_point"})
}
func (m *MappingBuilder) GeoShape(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "geo_shape"})
}

// DenseVector adds an ANN-indexed embedding field of dims dimensions —
// searched with SearchRequest.KNN / SearchBuilder.KNN (vector + hybrid
// search readiness). similarity is "cosine", "dot_product", or "l2_norm"
// ("cosine" when empty).
func (m *MappingBuilder) DenseVector(name string, dims int, similarity string) *MappingBuilder {
	if similarity == "" {
		similarity = "cosine"
	}
	return m.Field(name, map[string]any{
		"type": "dense_vector", "dims": dims, "index": true, "similarity": similarity,
	})
}

// Completion adds a completion-suggester field (CompletionSuggester).
func (m *MappingBuilder) Completion(name string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "completion"})
}

// Object adds a sub-object with its own properties.
func (m *MappingBuilder) Object(name string, sub *MappingBuilder) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "object", "properties": sub.properties})
}

// NestedField adds a nested object array — each element indexed as its own
// hidden document so per-element conditions stay coherent (query with
// Nested(path, inner)).
func (m *MappingBuilder) NestedField(name string, sub *MappingBuilder) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "nested", "properties": sub.properties})
}

// Join declares a parent-child join field: relations maps parent type →
// child types, e.g. {"question": {"answer"}}. Query with HasChild /
// HasParent / ParentID. Children must be routed to the parent's shard.
func (m *MappingBuilder) Join(name string, relations map[string][]string) *MappingBuilder {
	return m.Field(name, map[string]any{"type": "join", "relations": relations})
}

// RuntimeField defines a script-computed field evaluated at query time —
// schema changes without reindexing, at per-query cost. kind is the runtime
// type ("keyword", "long", "date", …).
func (m *MappingBuilder) RuntimeField(name, kind, script string) *MappingBuilder {
	if m.runtime == nil {
		m.runtime = map[string]any{}
	}
	m.runtime[name] = map[string]any{"type": kind, "script": map[string]any{"source": script}}
	return m
}

// DynamicTemplate adds a dynamic template controlling how unmapped fields
// get typed, e.g. DynamicTemplate("strings_as_keyword",
// map[string]any{"match_mapping_type": "string", "mapping": map[string]any{"type": "keyword"}}).
func (m *MappingBuilder) DynamicTemplate(name string, template map[string]any) *MappingBuilder {
	m.dynamicTemplates = append(m.dynamicTemplates, map[string]any{name: template})
	return m
}

// ── analysis settings ───────────────────────────────────────────────────────

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

// Build renders the {settings, mappings} body for CreateIndex / EnsureIndex /
// MigrateIndex.
func (m *MappingBuilder) Build() map[string]any {
	mappings := map[string]any{"properties": m.properties}
	if m.dynamic != "" {
		mappings["dynamic"] = m.dynamic
	}
	if len(m.dynamicTemplates) > 0 {
		mappings["dynamic_templates"] = m.dynamicTemplates
	}
	if len(m.runtime) > 0 {
		mappings["runtime"] = m.runtime
	}

	body := map[string]any{"mappings": mappings}

	settings := map[string]any{}
	for k, v := range m.settings {
		settings[k] = v
	}
	analysis := map[string]any{}
	if len(m.analyzers) > 0 {
		analysis["analyzer"] = m.analyzers
	}
	if len(m.tokenizers) > 0 {
		analysis["tokenizer"] = m.tokenizers
	}
	if len(m.filters) > 0 {
		analysis["filter"] = m.filters
	}
	if len(m.normalizer) > 0 {
		analysis["normalizer"] = m.normalizer
	}
	if len(analysis) > 0 {
		settings["analysis"] = analysis
	}
	if len(settings) > 0 {
		body["settings"] = settings
	}
	return body
}

// ── automatic mapping generation ────────────────────────────────────────────

// AutoMap derives a MappingBuilder from T's exported fields — a reviewed
// starting point, not a hidden runtime dependency: generate, inspect, adjust,
// commit. Rules:
//
//   - field names follow the json tag (fields tagged "-" are skipped)
//   - string → text + .keyword multi-field; bool → boolean
//   - integers → long; floats → double; time.Time → date
//   - []T of a scalar maps like the scalar (ES fields are lists natively)
//   - []struct → nested; struct → object (recursive); map → dynamic object
//   - an `es` tag overrides the type: es:"keyword", es:"geo_point",
//     es:"text", es:"date", …; es:"-" skips the field
func AutoMap[T any]() *MappingBuilder {
	m := NewMapping()
	var zero T
	autoMapStruct(reflect.TypeOf(zero), m)
	return m
}

func autoMapStruct(t reflect.Type, m *MappingBuilder) {
	if t == nil || t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := jsonFieldName(f)
		if name == "" {
			continue
		}
		if es := f.Tag.Get("es"); es != "" {
			if es == "-" {
				continue
			}
			m.Field(name, map[string]any{"type": es})
			continue
		}
		mapFieldType(name, f.Type, m)
	}
}

func mapFieldType(name string, t reflect.Type, m *MappingBuilder) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == reflect.TypeOf(time.Time{}):
		m.Date(name)
	case t.Kind() == reflect.String:
		m.TextWithKeyword(name)
	case t.Kind() == reflect.Bool:
		m.Boolean(name)
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Uint64:
		m.Long(name)
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		m.Double(name)
	case t.Kind() == reflect.Slice || t.Kind() == reflect.Array:
		elem := t.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct && elem != reflect.TypeOf(time.Time{}) {
			sub := NewMapping()
			autoMapStruct(elem, sub)
			m.NestedField(name, sub)
		} else {
			mapFieldType(name, elem, m) // ES fields are lists natively
		}
	case t.Kind() == reflect.Struct:
		sub := NewMapping()
		autoMapStruct(t, sub)
		m.Object(name, sub)
	case t.Kind() == reflect.Map:
		m.Field(name, map[string]any{"type": "object", "dynamic": true})
	default:
		// interfaces / funcs / channels have no sensible mapping — skip.
	}
}

func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return ""
	}
	if name == "" {
		return f.Name
	}
	return name
}

// Validate sanity-checks a built body (cheap guard for tests/boot): every
// property must carry a type or sub-properties.
func ValidateMapping(body map[string]any) error {
	mappings, _ := body["mappings"].(map[string]any)
	props, _ := mappings["properties"].(map[string]any)
	for name, p := range props {
		prop, ok := p.(map[string]any)
		if !ok {
			return fmt.Errorf("elastic mapping: property %q is not an object", name)
		}
		if _, hasType := prop["type"]; !hasType {
			if _, hasProps := prop["properties"]; !hasProps {
				return fmt.Errorf("elastic mapping: property %q has neither type nor properties", name)
			}
		}
	}
	return nil
}
