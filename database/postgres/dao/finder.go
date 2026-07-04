// Fluent query DSL over BaseDAO — the criteria-style front end to the same
// SQL builder the named methods use:
//
//	rows, err := dao.Query().
//	    Where(query.Eq("status", "PENDING"), query.Eq("consumer_id", id)).
//	    OrderByDesc("updated_at").
//	    Limit(20).Offset(0).
//	    Find(ctx)
//
// Terminals: Find, One, Count, Exists, Page. All honor the DAO's soft-delete
// scope (Unscoped() the DAO first to bypass) and its transaction binding
// (build the Finder from a WithTx-bound DAO to run inside a transaction).

package dao

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Finder accumulates criteria and executes them against the DAO's table.
// Zero value is not usable; obtain one via BaseDAO.Query().
type Finder[T any] struct {
	dao        *BaseDAO[T]
	conditions []query.Condition
	orderBy    []query.OrderBy
	limit      int
	offset     int
	joins      []query.Join
	columns    []string
	groupBy    []string
	having     []query.Condition
}

// Query starts a fluent criteria query on the DAO's table.
func (d *BaseDAO[T]) Query() *Finder[T] {
	return &Finder[T]{dao: d}
}

// Where appends predicates (combined with AND). Build them with the
// package-level query constructors (query.Eq, query.In, query.And, ...) or a
// ConditionBuilder's Build() output.
func (f *Finder[T]) Where(conditions ...query.Condition) *Finder[T] {
	f.conditions = append(f.conditions, conditions...)
	return f
}

// OrderBy appends an ascending sort key.
func (f *Finder[T]) OrderBy(column string) *Finder[T] {
	f.orderBy = append(f.orderBy, query.OrderBy{Column: column})
	return f
}

// OrderByDesc appends a descending sort key.
func (f *Finder[T]) OrderByDesc(column string) *Finder[T] {
	f.orderBy = append(f.orderBy, query.OrderBy{Column: column, Desc: true})
	return f
}

// Limit caps the number of rows returned by Find (and sizes Page).
func (f *Finder[T]) Limit(n int) *Finder[T] {
	f.limit = n
	return f
}

// Offset skips n rows (Find and Page).
func (f *Finder[T]) Offset(n int) *Finder[T] {
	f.offset = n
	return f
}

// Join appends a static SQL join to the query — read-only, applies to
// Find/One/Page/Count/Exists only, never to Insert/Update/Delete (BaseDAO has
// no join-aware write path). Table/On are emitted verbatim by the underlying
// query.Join (see query.SQLBuilder's doc comment): supply only
// code-authored identifiers, never raw user input. Repeatable — e.g. two
// Join calls against the same table with different aliases for a self-join.
func (f *Finder[T]) Join(j query.Join) *Finder[T] {
	f.joins = append(f.joins, j)
	return f
}

// Select sets an explicit SELECT column list, replacing the default "*".
// Required whenever Join is used and either side needs its own NULL-handling
// for a LEFT JOIN's possibly-missing row, e.g.
//
//	Select("COALESCE(c.email_id, '') AS consumer_email")
//
// List every desired column, base-table and joined, exactly as hand-written
// SQL or sqlc would; there is no implicit "<table>.*" added on top. T must
// declare matching exported fields for every listed column
// (pgx.RowToStructByNameLax).
func (f *Finder[T]) Select(cols ...string) *Finder[T] {
	f.columns = append(f.columns, cols...)
	return f
}

// GroupBy appends columns/expressions to a GROUP BY clause — combine with
// Select to list the grouped columns plus aggregate expressions (e.g.
// "COUNT(*) AS total"). Emitted verbatim — same trust boundary as
// Join/OrderBy: code-authored identifiers only, never raw user input.
// GroupBy columns must include every non-aggregated column named in Select
// (ordinary SQL rule, not enforced here). Applies to Find/One/Page only —
// Count/Exists assume a single scalar/existence result, which a grouped
// query doesn't produce.
func (f *Finder[T]) GroupBy(cols ...string) *Finder[T] {
	f.groupBy = append(f.groupBy, cols...)
	return f
}

// Having appends post-aggregation filter predicates, rendered after GROUP BY
// — the same query.Condition model Where uses for WHERE. Same Find/One/Page
// scope as GroupBy.
func (f *Finder[T]) Having(conditions ...query.Condition) *Finder[T] {
	f.having = append(f.having, conditions...)
	return f
}

// selectColumns returns nil (SELECT *) until Select is called — zero
// behavior change for every existing Finder caller — or the explicit column
// list Select accumulated.
func (f *Finder[T]) selectColumns() []string {
	if len(f.columns) == 0 {
		return nil
	}
	return f.columns
}

// Find executes the query and returns all matching rows.
func (f *Finder[T]) Find(ctx context.Context) ([]T, error) {
	q := query.SelectQuery{
		Table:      f.dao.TableName,
		Columns:    f.selectColumns(),
		Joins:      f.joins,
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
		GroupBy:    f.groupBy,
		Having:     f.having,
		OrderBy:    f.orderBy,
		Limit:      f.limit,
		Offset:     f.offset,
	}
	sql, args := f.dao.builder.BuildSelect(q)
	return f.dao.selectMany(ctx, sql, args)
}

// One executes the query with LIMIT 1 and returns the first row
// (honoring OrderBy), or a NotFound error.
func (f *Finder[T]) One(ctx context.Context) (*T, error) {
	q := query.SelectQuery{
		Table:      f.dao.TableName,
		Columns:    f.selectColumns(),
		Joins:      f.joins,
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
		GroupBy:    f.groupBy,
		Having:     f.having,
		OrderBy:    f.orderBy,
		Limit:      1,
		Offset:     f.offset,
	}
	sql, args := f.dao.builder.BuildSelect(q)
	return f.dao.selectOne(ctx, sql, args)
}

// Count returns the number of matching rows (ignores Limit/Offset/OrderBy).
func (f *Finder[T]) Count(ctx context.Context) (int64, error) {
	if len(f.joins) == 0 {
		return f.dao.Count(ctx, f.conditions)
	}
	q := query.SelectQuery{
		Table:      f.dao.TableName,
		Columns:    []string{"COUNT(*) AS count"},
		Joins:      f.joins,
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
	}
	sql, args := f.dao.builder.BuildSelect(q)
	var count int64
	if err := f.dao.DB.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, MapPgError(err)
	}
	return count, nil
}

// Exists reports whether any row matches (ignores Limit/Offset/OrderBy).
func (f *Finder[T]) Exists(ctx context.Context) (bool, error) {
	if len(f.joins) == 0 && len(f.columns) == 0 {
		return f.dao.Exists(ctx, f.conditions)
	}
	q := query.SelectQuery{
		Table:      f.dao.TableName,
		Columns:    f.selectColumns(),
		Joins:      f.joins,
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
		Limit:      1,
	}
	sql, args := f.dao.builder.BuildSelect(q)
	_, err := f.dao.selectOne(ctx, sql, args)
	if err == nil {
		return true, nil
	}
	if dxerrors.IsNotFoundError(err) {
		return false, nil
	}
	return false, err
}

// Page executes the query as one page plus the total match count.
// Limit defaults per FindPage when unset.
func (f *Finder[T]) Page(ctx context.Context) (*Page[T], error) {
	if len(f.joins) == 0 && len(f.columns) == 0 && len(f.groupBy) == 0 && len(f.having) == 0 {
		return f.dao.FindPage(ctx, f.conditions, f.orderBy, f.limit, f.offset)
	}

	limit, offset := f.limit, f.offset
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	total, err := f.Count(ctx)
	if err != nil {
		return nil, err
	}

	page := &Page[T]{Limit: limit, Offset: offset, Total: total, Data: []T{}}
	if total > int64(offset) {
		q := query.SelectQuery{
			Table:      f.dao.TableName,
			Columns:    f.selectColumns(),
			Joins:      f.joins,
			Conditions: f.dao.withSoftDeleteFilter(f.conditions),
			GroupBy:    f.groupBy,
			Having:     f.having,
			OrderBy:    f.orderBy,
			Limit:      limit,
			Offset:     offset,
		}
		sql, args := f.dao.builder.BuildSelect(q)
		data, err := f.dao.selectMany(ctx, sql, args)
		if err != nil {
			return nil, err
		}
		page.Data = data
	}
	page.HasNext = int64(offset+len(page.Data)) < total
	return page, nil
}
