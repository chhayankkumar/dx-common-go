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
