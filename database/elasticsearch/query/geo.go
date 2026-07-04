package query

// GeoBoundingBox matches geo_point documents inside the lat/lon box defined by
// its top-left and bottom-right corners.
func GeoBoundingBox(field string, topLeftLat, topLeftLon, bottomRightLat, bottomRightLon float64) Query {
	return Query{"geo_bounding_box": map[string]any{
		field: map[string]any{
			"top_left":     map[string]any{"lat": topLeftLat, "lon": topLeftLon},
			"bottom_right": map[string]any{"lat": bottomRightLat, "lon": bottomRightLon},
		},
	}}
}

// GeoDistance matches geo_point documents within distance (e.g. "5km") of the
// center point.
func GeoDistance(field string, lat, lon float64, distance string) Query {
	return Query{"geo_distance": map[string]any{
		"distance": distance,
		field:      map[string]any{"lat": lat, "lon": lon},
	}}
}

// GeoShape matches documents whose geo_shape field relates to the given GeoJSON
// shape. shapeType is "point"/"polygon"/"linestring"/"envelope"; relation is
// "within"/"intersects"/"contains"/"disjoint". coordinates is the GeoJSON
// coordinates array for the shape type.
func GeoShape(field, shapeType string, coordinates any, relation string) Query {
	return Query{"geo_shape": map[string]any{
		field: map[string]any{
			"shape":    map[string]any{"type": shapeType, "coordinates": coordinates},
			"relation": relation,
		},
	}}
}

// ScriptScore wraps inner with a custom Painless script score — e.g. cosine
// similarity over a dense_vector for NLP/semantic search. params are exposed to
// the script as `params`.
func ScriptScore(inner Query, source string, params map[string]any) Query {
	script := map[string]any{"source": source}
	if len(params) > 0 {
		script["params"] = params
	}
	return Query{"script_score": map[string]any{"query": inner, "script": script}}
}

// FunctionScoreBuilder composes a function_score query: a base query whose
// relevance is reshaped by scoring functions.
type FunctionScoreBuilder struct {
	query     Query
	functions []map[string]any
	scoreMode string
	boostMode string
}

// FunctionScore starts a function_score over query (nil = match_all).
func FunctionScore(query Query) *FunctionScoreBuilder {
	return &FunctionScoreBuilder{query: query}
}

// FieldValueFactor multiplies relevance by a document field, e.g.
// FieldValueFactor("popularity", 1.2, "log1p", 1) — modifier may be "" (none),
// missing is the value used when the field is absent.
func (f *FunctionScoreBuilder) FieldValueFactor(field string, factor float64, modifier string, missing float64) *FunctionScoreBuilder {
	fn := map[string]any{"field": field, "factor": factor, "missing": missing}
	if modifier != "" {
		fn["modifier"] = modifier
	}
	f.functions = append(f.functions, map[string]any{"field_value_factor": fn})
	return f
}

// Weight boosts documents matching filter by weight (filter nil = all).
func (f *FunctionScoreBuilder) Weight(weight float64, filter Query) *FunctionScoreBuilder {
	fn := map[string]any{"weight": weight}
	if filter != nil {
		fn["filter"] = filter
	}
	f.functions = append(f.functions, fn)
	return f
}

// Decay adds a decay function: kind is "gauss", "exp", or "linear"; origin/
// scale follow the field's type (dates: "now" / "10d"; geo: point / "2km").
func (f *FunctionScoreBuilder) Decay(kind, field string, origin, scale any) *FunctionScoreBuilder {
	f.functions = append(f.functions, map[string]any{kind: map[string]any{field: map[string]any{"origin": origin, "scale": scale}}})
	return f
}

// ScoreMode sets how function results combine: "multiply" (default), "sum",
// "avg", "first", "max", "min".
func (f *FunctionScoreBuilder) ScoreMode(mode string) *FunctionScoreBuilder {
	f.scoreMode = mode
	return f
}

// BoostMode sets how the combined function score merges with the query score:
// "multiply" (default), "replace", "sum", "avg", "max", "min".
func (f *FunctionScoreBuilder) BoostMode(mode string) *FunctionScoreBuilder {
	f.boostMode = mode
	return f
}

// Build renders the function_score query.
func (f *FunctionScoreBuilder) Build() Query {
	body := map[string]any{}
	if f.query != nil {
		body["query"] = f.query
	}
	if len(f.functions) > 0 {
		body["functions"] = f.functions
	}
	if f.scoreMode != "" {
		body["score_mode"] = f.scoreMode
	}
	if f.boostMode != "" {
		body["boost_mode"] = f.boostMode
	}
	return Query{"function_score": body}
}
