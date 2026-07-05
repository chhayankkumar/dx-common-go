package dao

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// FindPage fetches one page of rows together with the total match count
// (count query + page query over the same conditions), the Go counterpart
// of the Java paginated select.
func (d *BaseDAO[T]) FindPage(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy, limit, offset int) (*Page[T], error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	// Count and the page query below each apply the soft-delete filter once
	// themselves — pre-applying it here as well appended the same predicate
	// (and bind arg) twice to both statements.
	total, err := d.Count(ctx, conditions)
	if err != nil {
		return nil, err
	}

	page := &Page[T]{Limit: limit, Offset: offset, Total: total, Data: []T{}}
	if total > int64(offset) {
		q := query.SelectQuery{
			Table:      d.TableName,
			Conditions: d.withSoftDeleteFilter(conditions),
			OrderBy:    orderBy,
			Limit:      limit,
			Offset:     offset,
		}
		sql, args := d.builder.BuildSelect(q)
		data, err := d.selectMany(ctx, sql, args)
		if err != nil {
			return nil, err
		}
		page.Data = data
	}
	page.HasNext = int64(offset+len(page.Data)) < total
	return page, nil
}

// CountBy groups rows by column and returns the row count for each distinct
// value — the grouped-count aggregate services otherwise hand-roll (e.g. "how
// many requests per status"). It honors d's soft-delete scope and transaction
// binding.
//
// K must match the column's scanned Go type (string for text/enum columns,
// int64 for integers, and so on); a mismatch surfaces as a scan error. column
// is emitted verbatim into the SELECT and GROUP BY — supply a code-authored
// identifier, never raw user input.
//
// It is a package-level function rather than a BaseDAO method because the key
// type K is independent of the DAO's row type T, and Go methods cannot declare
// their own type parameters.
func CountBy[K comparable, T any](ctx context.Context, d *BaseDAO[T], column string, conditions ...query.Condition) (map[K]int64, error) {
	q := query.SelectQuery{
		Table:      d.TableName,
		Columns:    []string{column, "COUNT(*) AS count"},
		Conditions: d.withSoftDeleteFilter(conditions),
		GroupBy:    []string{column},
	}
	sql, args := d.builder.BuildSelect(q)

	rows, err := d.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, MapPgError(err)
	}
	defer rows.Close()

	result := make(map[K]int64)
	for rows.Next() {
		var key K
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, MapPgError(err)
		}
		result[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, MapPgError(err)
	}
	return result, nil
}

// Count returns the number of rows matching conditions.
func (d *BaseDAO[T]) Count(ctx context.Context, conditions []query.Condition) (int64, error) {
	q := query.SelectQuery{
		Table:      d.TableName,
		Columns:    []string{"COUNT(*) AS count"},
		Conditions: d.withSoftDeleteFilter(conditions),
	}
	sql, args := d.builder.BuildSelect(q)

	var count int64
	if err := d.DB.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, MapPgError(err)
	}
	return count, nil
}
