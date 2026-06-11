// Package dao provides a generic, transaction-aware data-access layer over
// pgx, mirroring the Java dx-common AbstractBaseDAO pattern: a concrete DAO
// for a new table is one constructor call, with CRUD, filtered queries and
// COUNT(*) OVER() pagination inherited.
//
// Struct mapping uses pgx.RowToStructByName: exported fields must match
// column names (use `db:"column"` struct tags for differing names).
package dao

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// Querier is the subset of pgx behaviour the DAO needs. Both *pgxpool.Pool
// and pgx.Tx satisfy it, so any DAO method can run inside a transaction via
// WithTx without duplicate code paths.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Page is a paginated result set (mirrors the Java PaginatedResult).
type Page[T any] struct {
	Data    []T   `json:"data"`
	Total   int64 `json:"totalHits"`
	Limit   int   `json:"limit"`
	Offset  int   `json:"offset"`
	HasNext bool  `json:"hasNext"`
}

// BaseDAO provides generic CRUD operations for a single database table.
type BaseDAO[T any] struct {
	DB        Querier
	TableName string
	// IDColumn is the primary-key column used by FindByID/SoftDelete. Defaults to "id".
	IDColumn string
	builder  *query.SQLBuilder
}

// NewBaseDAO creates a BaseDAO for the given table. db is usually a
// *pgxpool.Pool; pass a pgx.Tx (or use WithTx) for transactional use.
func NewBaseDAO[T any](db Querier, tableName string) *BaseDAO[T] {
	return &BaseDAO[T]{DB: db, TableName: tableName, IDColumn: "id", builder: query.New()}
}

// WithTx returns a shallow copy of the DAO bound to the given transaction.
// All operations on the returned DAO participate in tx.
func (d *BaseDAO[T]) WithTx(tx pgx.Tx) *BaseDAO[T] {
	clone := *d
	clone.DB = tx
	return &clone
}

// FindByID fetches a single row by its primary-key column.
func (d *BaseDAO[T]) FindByID(ctx context.Context, id string) (*T, error) {
	q := query.SelectQuery{
		Table:      d.TableName,
		Conditions: query.NewConditionBuilder().Eq(d.IDColumn, id).Build(),
		Limit:      1,
	}
	sql, args := d.builder.BuildSelect(q)
	return d.selectOne(ctx, sql, args)
}

// FindOne fetches the first row matching conditions.
func (d *BaseDAO[T]) FindOne(ctx context.Context, conditions []query.Condition) (*T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: conditions, Limit: 1}
	sql, args := d.builder.BuildSelect(q)
	return d.selectOne(ctx, sql, args)
}

// FindAll fetches all rows matching the provided conditions (empty means all).
func (d *BaseDAO[T]) FindAll(ctx context.Context, conditions []query.Condition) ([]T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: conditions}
	sql, args := d.builder.BuildSelect(q)
	return d.selectMany(ctx, sql, args)
}

// FindAllOrdered fetches all matching rows in the given order (no pagination).
func (d *BaseDAO[T]) FindAllOrdered(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy) ([]T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: conditions, OrderBy: orderBy}
	sql, args := d.builder.BuildSelect(q)
	return d.selectMany(ctx, sql, args)
}

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

	total, err := d.Count(ctx, conditions)
	if err != nil {
		return nil, err
	}

	page := &Page[T]{Limit: limit, Offset: offset, Total: total, Data: []T{}}
	if total > int64(offset) {
		q := query.SelectQuery{
			Table:      d.TableName,
			Conditions: conditions,
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
		Conditions: conditions,
	}
	sql, args := d.builder.BuildSelect(q)

	var count int64
	if err := d.DB.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, MapPgError(err)
	}
	return count, nil
}

// Insert inserts a row using the provided column names and corresponding values.
func (d *BaseDAO[T]) Insert(ctx context.Context, columns []string, values []any) error {
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values}
	sql, args := d.builder.BuildInsert(q)

	if _, err := d.DB.Exec(ctx, sql, args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// InsertMap inserts the non-nil fields of m (column → value, the Go
// equivalent of the Java toNonEmptyFieldsMap flow) and returns the stored
// row via RETURNING *.
func (d *BaseDAO[T]) InsertMap(ctx context.Context, m map[string]any) (*T, error) {
	columns, values := splitMap(m)
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values, Returning: []string{"*"}}
	sql, args := d.builder.BuildInsert(q)
	return d.selectOne(ctx, sql, args)
}

// Update applies SET assignments to all rows matching conditions.
func (d *BaseDAO[T]) Update(ctx context.Context, set map[string]any, conditions []query.Condition) error {
	q := query.UpdateQuery{Table: d.TableName, Set: set, Conditions: conditions}
	sql, args := d.builder.BuildUpdate(q)

	if _, err := d.DB.Exec(ctx, sql, args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// UpdateReturning applies SET assignments and returns the first updated row.
// Returns NotFound when no row matched.
func (d *BaseDAO[T]) UpdateReturning(ctx context.Context, set map[string]any, conditions []query.Condition) (*T, error) {
	q := query.UpdateQuery{Table: d.TableName, Set: set, Conditions: conditions, Returning: []string{"*"}}
	sql, args := d.builder.BuildUpdate(q)
	return d.selectOne(ctx, sql, args)
}

// Upsert inserts m, updating updateColumns on conflictColumn conflicts, and
// returns the stored row.
func (d *BaseDAO[T]) Upsert(ctx context.Context, m map[string]any, conflictColumn string, updateColumns []string) (*T, error) {
	columns, values := splitMap(m)
	q := query.UpsertQuery{
		Table:          d.TableName,
		Columns:        columns,
		Values:         values,
		ConflictColumn: conflictColumn,
		UpdateColumns:  updateColumns,
		Returning:      []string{"*"},
	}
	sql, args := d.builder.BuildUpsert(q)
	return d.selectOne(ctx, sql, args)
}

// SoftDelete sets status='DELETED' on the row with the given id.
func (d *BaseDAO[T]) SoftDelete(ctx context.Context, id string) error {
	q := query.DeleteQuery{
		Table:      d.TableName,
		Conditions: query.NewConditionBuilder().Eq(d.IDColumn, id).Build(),
		SoftDelete: true,
	}
	sql, args := d.builder.BuildDelete(q)

	tag, err := d.DB.Exec(ctx, sql, args...)
	if err != nil {
		return MapPgError(err)
	}
	if tag.RowsAffected() == 0 {
		return MapPgError(pgx.ErrNoRows)
	}
	return nil
}

// HardDelete permanently deletes rows matching conditions.
func (d *BaseDAO[T]) HardDelete(ctx context.Context, conditions []query.Condition) error {
	q := query.DeleteQuery{Table: d.TableName, Conditions: conditions}
	sql, args := d.builder.BuildDelete(q)

	if _, err := d.DB.Exec(ctx, sql, args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// InsertReturning inserts a row and scans the RETURNING clause into dest.
func (d *BaseDAO[T]) InsertReturning(ctx context.Context, columns []string, values []any, returning []string, dest ...any) error {
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values, Returning: returning}
	sql, args := d.builder.BuildInsert(q)

	if err := d.DB.QueryRow(ctx, sql, args...).Scan(dest...); err != nil {
		return fmt.Errorf("InsertReturning: %w", MapPgError(err))
	}
	return nil
}

// Select is the raw-SQL escape hatch for queries the builder cannot express
// (CTEs, window functions, jsonb aggregation). Rows are mapped to T by name
// and errors are translated through MapPgError, so hand-written SQL still
// shares the DAO's scanning and error semantics.
func (d *BaseDAO[T]) Select(ctx context.Context, sql string, args ...any) ([]T, error) {
	return d.selectMany(ctx, sql, args)
}

// SelectOne is Select for single-row queries; returns NotFound on no rows.
func (d *BaseDAO[T]) SelectOne(ctx context.Context, sql string, args ...any) (*T, error) {
	return d.selectOne(ctx, sql, args)
}

// Exec runs a raw statement through the DAO's error translation.
func (d *BaseDAO[T]) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	tag, err := d.DB.Exec(ctx, sql, args...)
	if err != nil {
		return 0, MapPgError(err)
	}
	return tag.RowsAffected(), nil
}

// ── internals ───────────────────────────────────────────────────────────────

func (d *BaseDAO[T]) selectOne(ctx context.Context, sql string, args []any) (*T, error) {
	rows, err := d.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, MapPgError(err)
	}
	defer rows.Close()

	result, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, MapPgError(err)
	}
	return &result, nil
}

func (d *BaseDAO[T]) selectMany(ctx context.Context, sql string, args []any) ([]T, error) {
	rows, err := d.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, MapPgError(err)
	}
	defer rows.Close()

	results, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, MapPgError(err)
	}
	return results, nil
}

func splitMap(m map[string]any) ([]string, []any) {
	columns := make([]string, 0, len(m))
	for col := range m {
		columns = append(columns, col)
	}
	sort.Strings(columns) // deterministic SQL
	values := make([]any, 0, len(columns))
	for _, col := range columns {
		values = append(values, m[col])
	}
	return columns, values
}
