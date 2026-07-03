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
)

// Finder accumulates criteria and executes them against the DAO's table.
// Zero value is not usable; obtain one via BaseDAO.Query().
type Finder[T any] struct {
	dao        *BaseDAO[T]
	conditions []query.Condition
	orderBy    []query.OrderBy
	limit      int
	offset     int
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

// Find executes the query and returns all matching rows.
func (f *Finder[T]) Find(ctx context.Context) ([]T, error) {
	q := query.SelectQuery{
		Table:      f.dao.TableName,
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
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
		Conditions: f.dao.withSoftDeleteFilter(f.conditions),
		OrderBy:    f.orderBy,
		Limit:      1,
		Offset:     f.offset,
	}
	sql, args := f.dao.builder.BuildSelect(q)
	return f.dao.selectOne(ctx, sql, args)
}

// Count returns the number of matching rows (ignores Limit/Offset/OrderBy).
func (f *Finder[T]) Count(ctx context.Context) (int64, error) {
	return f.dao.Count(ctx, f.conditions)
}

// Exists reports whether any row matches (ignores Limit/Offset/OrderBy).
func (f *Finder[T]) Exists(ctx context.Context) (bool, error) {
	return f.dao.Exists(ctx, f.conditions)
}

// Page executes the query as one page plus the total match count.
// Limit defaults per FindPage when unset.
func (f *Finder[T]) Page(ctx context.Context) (*Page[T], error) {
	return f.dao.FindPage(ctx, f.conditions, f.orderBy, f.limit, f.offset)
}
