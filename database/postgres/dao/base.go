// Package dao provides a generic, transaction-aware data-access layer over
// pgx, mirroring the Java dx-common AbstractBaseDAO pattern: a concrete DAO
// for a new table is one constructor call, with CRUD, filtered queries and
// COUNT(*) OVER() pagination inherited.
//
// Struct mapping uses pgx.RowToStructByName: exported fields must match
// column names (use `db:"column"` struct tags for differing names).
//
// This file holds BaseDAO's struct/constructors/options and its plain reads;
// writes/deletes/counting/batch operations live in their own topic files
// (insert.go, update.go, delete.go, count.go, exists.go, batch.go) within
// this same package — see each file's own doc comment.
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

	// softDeleteColumn, when set via WithSoftDeleteFilter, is auto-excluded
	// (column <> deleted-sentinel) from every Find*/Count call unless the DAO
	// was obtained through Unscoped(). The sentinel pair defaults to
	// DELETED/ACTIVE and is overridable via WithSoftDeleteValues.
	softDeleteColumn  string
	softDeleteDeleted string
	softDeleteActive  string
	unscoped          bool

	// auditCreatedBy/auditUpdatedBy, when set via WithAuditColumns, are
	// auto-populated from the context actor (WithActor) on map-based writes.
	auditCreatedBy string
	auditUpdatedBy string
}

// Option configures a BaseDAO at construction time, for use with NewBaseDAOWith.
type Option[T any] func(*BaseDAO[T])

// WithSoftDeleteFilter makes every Find*/Count call auto-exclude rows where
// column = 'DELETED' (the same sentinel BaseDAO.SoftDelete writes), the Go
// counterpart of a JPA/Hibernate @Where soft-delete filter. Opt-in — DAOs
// constructed without this option see no behavior change. Use Unscoped() to
// bypass the filter for one call chain (e.g. an admin "show deleted" view).
func WithSoftDeleteFilter[T any](column string) Option[T] {
	return func(d *BaseDAO[T]) { d.softDeleteColumn = column }
}

// WithIDColumn overrides the default "id" primary-key column used by
// FindByID/SoftDelete — for tables whose key isn't literally named "id"
// (e.g. request_id).
func WithIDColumn[T any](column string) Option[T] {
	return func(d *BaseDAO[T]) { d.IDColumn = column }
}

// NewBaseDAO creates a BaseDAO for the given table. db is usually a
// *pgxpool.Pool; pass a pgx.Tx (or use WithTx) for transactional use.
func NewBaseDAO[T any](db Querier, tableName string) *BaseDAO[T] {
	return &BaseDAO[T]{DB: db, TableName: tableName, IDColumn: "id", builder: query.New()}
}

// NewBaseDAOWith is NewBaseDAO plus construction-time Options (WithIDColumn,
// WithSoftDeleteFilter, …) — kept as a separate constructor rather than
// widening NewBaseDAO's signature, so every existing two-argument call site
// is untouched.
func NewBaseDAOWith[T any](db Querier, tableName string, opts ...Option[T]) *BaseDAO[T] {
	d := NewBaseDAO[T](db, tableName)
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// TableDescriber lets an entity self-describe its table name and ID column,
// so NewBaseDAOFromEntity needs no explicit tableName argument. Optional —
// NewBaseDAO/NewBaseDAOWith remain the primary, unaffected construction path
// for every entity that doesn't implement this.
type TableDescriber interface {
	TableName() string
	IDColumn() string
}

// NewBaseDAOFromEntity constructs a BaseDAO for an entity implementing
// TableDescriber, inferring TableName/IDColumn from it instead of requiring
// them as arguments. Panics if T implements neither TableDescriber nor
// *T does — a programmer error caught at first construction, not a runtime
// data condition, so a panic (rather than a returned error) matches how Go's
// own type-assertion failures behave.
func NewBaseDAOFromEntity[T any](db Querier, opts ...Option[T]) *BaseDAO[T] {
	var zero T
	td, ok := any(zero).(TableDescriber)
	if !ok {
		td, ok = any(&zero).(TableDescriber)
	}
	if !ok {
		panic(fmt.Sprintf("dao.NewBaseDAOFromEntity[%T]: does not implement dao.TableDescriber", zero))
	}
	d := NewBaseDAO[T](db, td.TableName())
	d.IDColumn = td.IDColumn()
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// WithTx returns a shallow copy of the DAO bound to the given transaction.
// All operations on the returned DAO participate in tx.
func (d *BaseDAO[T]) WithTx(tx pgx.Tx) *BaseDAO[T] {
	clone := *d
	clone.DB = tx
	return &clone
}

// Unscoped returns a shallow copy of the DAO with the soft-delete filter
// suspended for calls made on it. The receiver is unaffected.
func (d *BaseDAO[T]) Unscoped() *BaseDAO[T] {
	clone := *d
	clone.unscoped = true
	return &clone
}

// withSoftDeleteFilter appends the soft-delete exclusion to conditions when
// the DAO was constructed with WithSoftDeleteFilter and isn't Unscoped().
// The input slice is never mutated in place.
func (d *BaseDAO[T]) withSoftDeleteFilter(conditions []query.Condition) []query.Condition {
	if d.softDeleteColumn == "" || d.unscoped {
		return conditions
	}
	out := make([]query.Condition, 0, len(conditions)+1)
	out = append(out, conditions...)
	return append(out, query.Condition{Column: d.softDeleteColumn, Op: query.OpNotEq, Value: d.deletedValue()})
}

// FindByID fetches a single row by its primary-key column.
func (d *BaseDAO[T]) FindByID(ctx context.Context, id string) (*T, error) {
	q := query.SelectQuery{
		Table:      d.TableName,
		Conditions: d.withSoftDeleteFilter(query.NewConditionBuilder().Eq(d.IDColumn, id).Build()),
		Limit:      1,
	}
	sql, args := d.builder.BuildSelect(q)
	return d.selectOne(ctx, sql, args)
}

// FindOne fetches the first row matching conditions.
func (d *BaseDAO[T]) FindOne(ctx context.Context, conditions []query.Condition) (*T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: d.withSoftDeleteFilter(conditions), Limit: 1}
	sql, args := d.builder.BuildSelect(q)
	return d.selectOne(ctx, sql, args)
}

// FindAll fetches all rows matching the provided conditions (empty means all).
func (d *BaseDAO[T]) FindAll(ctx context.Context, conditions []query.Condition) ([]T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: d.withSoftDeleteFilter(conditions)}
	sql, args := d.builder.BuildSelect(q)
	return d.selectMany(ctx, sql, args)
}

// FindAllOrdered fetches all matching rows in the given order (no pagination).
func (d *BaseDAO[T]) FindAllOrdered(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy) ([]T, error) {
	q := query.SelectQuery{Table: d.TableName, Conditions: d.withSoftDeleteFilter(conditions), OrderBy: orderBy}
	sql, args := d.builder.BuildSelect(q)
	return d.selectMany(ctx, sql, args)
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

// ── internals (shared by every file in this package) ───────────────────────

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
