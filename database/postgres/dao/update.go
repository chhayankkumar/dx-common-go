package dao

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// Update applies SET assignments to all rows matching conditions.
func (d *BaseDAO[T]) Update(ctx context.Context, set map[string]any, conditions []query.Condition) error {
	set = d.auditUpdate(ctx, set)
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
	set = d.auditUpdate(ctx, set)
	q := query.UpdateQuery{Table: d.TableName, Set: set, Conditions: conditions, Returning: []string{"*"}}
	sql, args := d.builder.BuildUpdate(q)
	return d.selectOne(ctx, sql, args)
}

// Upsert inserts m, updating updateColumns on conflictColumn conflicts, and
// returns the stored row.
func (d *BaseDAO[T]) Upsert(ctx context.Context, m map[string]any, conflictColumn string, updateColumns []string) (*T, error) {
	m = d.auditInsert(ctx, m)
	updateColumns = d.auditUpdateColumns(updateColumns)
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

// UpdateByIDs applies set to every row whose IDColumn is in ids (bulk update
// by key; audit auto-population applies). Empty ids is a no-op.
func (d *BaseDAO[T]) UpdateByIDs(ctx context.Context, ids []string, set map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return d.Update(ctx, set, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}
