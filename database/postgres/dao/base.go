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
	"errors"
	"fmt"
	"sort"
	"strings"

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
	// (column <> 'DELETED') from every Find*/Count call unless the DAO was
	// obtained through Unscoped().
	softDeleteColumn string
	unscoped         bool
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
	return append(out, query.Condition{Column: d.softDeleteColumn, Op: query.OpNotEq, Value: "DELETED"})
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

// FindPage fetches one page of rows together with the total match count
// (count query + page query over the same conditions), the Go counterpart
// of the Java paginated select.
func (d *BaseDAO[T]) FindPage(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy, limit, offset int) (*Page[T], error) {
	conditions = d.withSoftDeleteFilter(conditions)
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

// Insert inserts a row using the provided column names and corresponding values.
func (d *BaseDAO[T]) Insert(ctx context.Context, columns []string, values []any) error {
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values}
	sql, args := d.builder.BuildInsert(q)

	if _, err := d.DB.Exec(ctx, sql, args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// InsertIgnore inserts a row, doing nothing if conflictColumn's value
// already exists (INSERT ... ON CONFLICT (conflictColumn) DO NOTHING) — the
// idempotent-insert pattern for a naturally-keyed row from an
// at-least-once delivery source (e.g. a message envelope's own id as the
// primary key: redelivery after a lost ack must not duplicate the row, and
// there's nothing meaningful to update on the "conflict" since it's the
// exact same record, not a real update). Returns inserted=true only when a
// new row was actually written.
func (d *BaseDAO[T]) InsertIgnore(ctx context.Context, columns []string, values []any, conflictColumn string) (inserted bool, err error) {
	if len(columns) != len(values) {
		return false, fmt.Errorf("dao.InsertIgnore: %d columns but %d values", len(columns), len(values))
	}
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING",
		d.TableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "), conflictColumn)

	tag, err := d.DB.Exec(ctx, sql, values...)
	if err != nil {
		return false, MapPgError(err)
	}
	return tag.RowsAffected() > 0, nil
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

// copier is satisfied by *pgxpool.Pool and pgx.Tx (both support the binary
// COPY protocol) but not by the minimal Querier interface, so CopyFrom
// type-asserts for it rather than widening Querier for every implementer.
type copier interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// CopyFrom bulk-inserts rows via PostgreSQL's binary COPY protocol — far
// faster than row-by-row INSERT for large batches, at the cost of not
// supporting RETURNING, ON CONFLICT, or triggers that only fire on INSERT.
// The underlying connection (pool or tx) must support CopyFrom; a DAO bound
// to a Querier that doesn't (e.g. a test double) returns an error.
func (d *BaseDAO[T]) CopyFrom(ctx context.Context, columns []string, rows [][]any) (int64, error) {
	cp, ok := d.DB.(copier)
	if !ok {
		return 0, fmt.Errorf("dao.CopyFrom: underlying connection does not support CopyFrom")
	}
	n, err := cp.CopyFrom(ctx, pgx.Identifier{d.TableName}, columns, pgx.CopyFromRows(rows))
	if err != nil {
		return 0, MapPgError(err)
	}
	return n, nil
}

// InsertMany inserts multiple rows in one multi-VALUES statement. Prefer
// CopyFrom for large batches; use InsertMany when the table has an
// INSERT-only trigger or the batch is small enough that COPY's setup cost
// isn't worth it.
func (d *BaseDAO[T]) InsertMany(ctx context.Context, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "INSERT INTO %s (%s) VALUES ", d.TableName, strings.Join(columns, ", "))
	args := make([]any, 0, len(rows)*len(columns))
	idx := 1
	for i, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("dao.InsertMany: row %d has %d values, want %d", i, len(row), len(columns))
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for j := range row {
			if j > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "$%d", idx)
			idx++
		}
		sb.WriteByte(')')
		args = append(args, row...)
	}

	if _, err := d.DB.Exec(ctx, sb.String(), args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// UpdateVersioned applies an optimistic-locking update: set is applied
// together with versionCol = versionCol + 1, gated on
// versionCol = expected. Zero rows affected — the row doesn't exist, or was
// concurrently modified since the caller read expected — returns
// ErrStaleVersion rather than the generic NotFound UpdateReturning would give.
func (d *BaseDAO[T]) UpdateVersioned(ctx context.Context, set map[string]any, conditions []query.Condition, versionCol string, expected int64) (*T, error) {
	guarded := make([]query.Condition, 0, len(conditions)+1)
	guarded = append(guarded, conditions...)
	guarded = append(guarded, query.Condition{Column: versionCol, Op: query.OpEq, Value: expected})

	q := query.UpdateQuery{
		Table:      d.TableName,
		Set:        set,
		Increment:  []string{versionCol},
		Conditions: guarded,
		Returning:  []string{"*"},
	}
	sql, args := d.builder.BuildUpdate(q)

	rows, err := d.DB.Query(ctx, sql, args...)
	if err != nil {
		return nil, MapPgError(err)
	}
	defer rows.Close()

	result, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStaleVersion
		}
		return nil, MapPgError(err)
	}
	return &result, nil
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
