// W9 persistence-framework extensions (GO-STANDARDS-ROLLOUT_PLAN Part B R3):
// existence/multi-id lookups, PostgreSQL-optimised bulk operations, opt-in
// optimistic locking, and configurable soft-delete with scoped reads +
// Restore. All additions compose with the existing Querier/builder so they
// work identically on a pool or inside WithTx.
package dao

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// ErrStaleVersion is returned by UpdateVersioned when no row matched the
// id+version pair — the row was updated (or deleted) by a concurrent writer.
var ErrStaleVersion = errors.New("stale version: row was modified concurrently")

// Option configures a BaseDAO at construction (backward-compatible variadic).
type Option[T any] func(*BaseDAO[T])

// WithIDColumn overrides the primary-key column (default "id").
func WithIDColumn[T any](col string) Option[T] {
	return func(d *BaseDAO[T]) { d.IDColumn = col }
}

// WithSoftDelete configures value-based soft delete on column (e.g.
// "status", deleted "DELETED", active "ACTIVE") and enables automatic
// filtering of deleted rows on Find*/Count/FindPage. Use Unscoped() to read
// deleted rows; Restore() reverses a soft delete; HardDelete stays permanent.
func WithSoftDelete[T any](column, deletedValue, activeValue string) Option[T] {
	return func(d *BaseDAO[T]) {
		d.softDelete = &softDeleteCfg{column: column, deleted: deletedValue, active: activeValue, filter: true}
	}
}

// NewBaseDAOWith creates a BaseDAO with options applied. (NewBaseDAO is kept
// unchanged for the existing fleet; this is the extended constructor.)
func NewBaseDAOWith[T any](db Querier, tableName string, opts ...Option[T]) *BaseDAO[T] {
	d := NewBaseDAO[T](db, tableName)
	for _, o := range opts {
		o(d)
	}
	return d
}

type softDeleteCfg struct {
	column  string
	deleted string
	active  string
	filter  bool
}

// scope injects the not-deleted predicate when soft-delete filtering is on.
func (d *BaseDAO[T]) scope(conditions []query.Condition) []query.Condition {
	if d.softDelete == nil || !d.softDelete.filter {
		return conditions
	}
	nd := query.NewConditionBuilder().NotEq(d.softDelete.column, d.softDelete.deleted).Build()
	return append(nd, conditions...)
}

// Unscoped returns a clone that does NOT filter soft-deleted rows.
func (d *BaseDAO[T]) Unscoped() *BaseDAO[T] {
	clone := *d
	if d.softDelete != nil {
		cfg := *d.softDelete
		cfg.filter = false
		clone.softDelete = &cfg
	}
	return &clone
}

// Restore reverses a soft delete for id. Requires WithSoftDelete.
func (d *BaseDAO[T]) Restore(ctx context.Context, id string) error {
	if d.softDelete == nil {
		return fmt.Errorf("dao: Restore requires WithSoftDelete configuration")
	}
	return d.Update(ctx,
		map[string]any{d.softDelete.column: d.softDelete.active},
		query.NewConditionBuilder().Eq(d.IDColumn, id).Build())
}

// Exists reports whether any (non-deleted, when scoped) row matches.
func (d *BaseDAO[T]) Exists(ctx context.Context, conditions []query.Condition) (bool, error) {
	_, err := d.FindOne(ctx, conditions)
	if err == nil {
		return true, nil
	}
	if dxerrors.IsNotFoundError(err) {
		return false, nil
	}
	return false, err
}

// FindByIDs returns all rows whose IDColumn is in ids (order not guaranteed).
func (d *BaseDAO[T]) FindByIDs(ctx context.Context, ids []string) ([]T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return d.FindAll(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// UpdateByIDs applies set to every row whose IDColumn is in ids (bulk update).
func (d *BaseDAO[T]) UpdateByIDs(ctx context.Context, ids []string, set map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return d.Update(ctx, set, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// DeleteByIDs permanently deletes every row whose IDColumn is in ids (bulk delete).
func (d *BaseDAO[T]) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return d.HardDelete(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// UpdateVersioned applies set to the row with id ONLY if versionCol still
// equals expected, incrementing it atomically — opt-in optimistic locking.
// Returns the updated row, or ErrStaleVersion when a concurrent writer won.
func (d *BaseDAO[T]) UpdateVersioned(ctx context.Context, id string, versionCol string, expected int64, set map[string]any) (*T, error) {
	if versionCol == "" {
		versionCol = "version"
	}
	merged := make(map[string]any, len(set)+1)
	for k, v := range set {
		merged[k] = v
	}
	merged[versionCol] = expected + 1
	row, err := d.UpdateReturning(ctx, merged, query.NewConditionBuilder().
		Eq(d.IDColumn, id).Eq(versionCol, expected).Build())
	if err != nil {
		if dxerrors.IsNotFoundError(err) {
			return nil, ErrStaleVersion
		}
		return nil, err
	}
	return row, nil
}

// batcher is the optional pipelined-batch capability (pool and tx both have it).
type batcher interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// copier is the optional COPY capability (pool and tx both have it).
type copier interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// InsertBatch inserts rows (each a column→value map with identical keys) in
// one pipelined round trip via pgx.Batch — for medium batches (10s–1000s).
func (d *BaseDAO[T]) InsertBatch(ctx context.Context, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	b, ok := d.DB.(batcher)
	if !ok {
		return fmt.Errorf("dao: InsertBatch requires a pool or tx (got %T)", d.DB)
	}
	batch := &pgx.Batch{}
	for _, m := range rows {
		columns, values := splitMap(m)
		q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values}
		sql, args := d.builder.BuildInsert(q)
		batch.Queue(sql, args...)
	}
	res := b.SendBatch(ctx, batch)
	defer res.Close() //nolint:errcheck
	for range rows {
		if _, err := res.Exec(); err != nil {
			return MapPgError(err)
		}
	}
	return nil
}

// CopyInsert bulk-loads rows via PostgreSQL COPY — the fastest path for large
// batches (10k+). columns fixes the order; each row must match it.
func (d *BaseDAO[T]) CopyInsert(ctx context.Context, columns []string, rows [][]any) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	c, ok := d.DB.(copier)
	if !ok {
		return 0, fmt.Errorf("dao: CopyInsert requires a pool or tx (got %T)", d.DB)
	}
	n, err := c.CopyFrom(ctx, pgx.Identifier{d.TableName}, columns, pgx.CopyFromRows(rows))
	if err != nil {
		return 0, MapPgError(err)
	}
	return n, nil
}
