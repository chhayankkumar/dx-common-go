package dao

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

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

// DeleteByIDs permanently deletes every row whose IDColumn is in ids (bulk
// delete by key). Empty ids is a no-op.
func (d *BaseDAO[T]) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return d.HardDelete(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// Restore reverses a soft delete: sets the soft-delete column back to the
// active sentinel for id. Requires WithSoftDeleteFilter; the update runs
// Unscoped (the row being restored is, by definition, currently deleted).
func (d *BaseDAO[T]) Restore(ctx context.Context, id string) error {
	if d.softDeleteColumn == "" {
		return fmt.Errorf("dao: Restore requires WithSoftDeleteFilter configuration")
	}
	return d.Unscoped().Update(ctx,
		map[string]any{d.softDeleteColumn: d.activeValue()},
		query.NewConditionBuilder().Eq(d.IDColumn, id).Build())
}
