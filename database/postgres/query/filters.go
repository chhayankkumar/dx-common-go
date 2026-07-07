package query

import (
	"reflect"
	"sort"
)

// FromFilters converts a request-level filter map into WHERE conditions,
// mirroring the Java ConditionBuilder.fromFilters pattern:
//
//	scalar value  → column = value
//	slice value   → column = ANY(values)   (empty slices are skipped)
//	nil value     → skipped
//	"" value      → skipped (empty query params)
//
// Note: numeric/bool zero values (0, false) are NOT skipped — only nil and
// the empty string are treated as "no filter". If a 0 or false should mean
// "unset", use a pointer (*int, *bool) and omit it, or leave the key out of
// the map entirely.
//
// Keys become column names in the generated SQL and are NOT escaped, so
// callers MUST pass only trusted, literal column names — never raw user
// input — as keys. Values are always bound as parameters and are safe.
// Keys are emitted in sorted order so generated SQL is deterministic.
func FromFilters(filters map[string]any) []Condition {
	if len(filters) == 0 {
		return nil
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	conds := make([]Condition, 0, len(keys))
	for _, col := range keys {
		v := filters[col]
		if v == nil {
			continue
		}
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			if rv.Len() == 0 {
				continue
			}
			if rv.Len() == 1 {
				conds = append(conds, Condition{Column: col, Op: OpEq, Value: rv.Index(0).Interface()})
				continue
			}
			conds = append(conds, Condition{Column: col, Op: OpIn, Value: v})
		case reflect.String:
			if rv.String() == "" {
				continue
			}
			conds = append(conds, Condition{Column: col, Op: OpEq, Value: v})
		default:
			conds = append(conds, Condition{Column: col, Op: OpEq, Value: v})
		}
	}
	return conds
}

// TemporalFilter expresses a time-relation filter (mirrors the Java
// TemporalRequest): Rel is one of "between", "after", "before".
type TemporalFilter struct {
	Field string
	Rel   string
	Time  any
	End   any // only for "between"
}

// FromTemporal converts temporal filters into conditions.
func FromTemporal(filters []TemporalFilter) []Condition {
	conds := make([]Condition, 0, len(filters))
	for _, t := range filters {
		switch t.Rel {
		case "between":
			conds = append(conds, Condition{Column: t.Field, Op: OpBetween, Value: []any{t.Time, t.End}})
		case "after":
			conds = append(conds, Condition{Column: t.Field, Op: OpGt, Value: t.Time})
		case "before":
			conds = append(conds, Condition{Column: t.Field, Op: OpLt, Value: t.Time})
		}
	}
	return conds
}
