package repository

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// ── promoted generic CRUD (all transaction-propagation-aware) ──────────────

func (b *Base[R]) FindByID(ctx context.Context, id string) (*R, error) {
	return b.DAO(ctx).FindByID(ctx, id)
}

func (b *Base[R]) FindOne(ctx context.Context, conditions []query.Condition) (*R, error) {
	return b.DAO(ctx).FindOne(ctx, conditions)
}

func (b *Base[R]) FindAll(ctx context.Context, conditions []query.Condition) ([]R, error) {
	return b.DAO(ctx).FindAll(ctx, conditions)
}

func (b *Base[R]) FindAllOrdered(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy) ([]R, error) {
	return b.DAO(ctx).FindAllOrdered(ctx, conditions, orderBy)
}

func (b *Base[R]) InsertMap(ctx context.Context, m map[string]any) (*R, error) {
	return b.DAO(ctx).InsertMap(ctx, m)
}

func (b *Base[R]) Insert(ctx context.Context, columns []string, values []any) error {
	return b.DAO(ctx).Insert(ctx, columns, values)
}

func (b *Base[R]) InsertIgnore(ctx context.Context, columns []string, values []any, conflictColumn string) (bool, error) {
	return b.DAO(ctx).InsertIgnore(ctx, columns, values, conflictColumn)
}

func (b *Base[R]) InsertReturning(ctx context.Context, columns []string, values []any, returning []string, dest ...any) error {
	return b.DAO(ctx).InsertReturning(ctx, columns, values, returning, dest...)
}

func (b *Base[R]) Update(ctx context.Context, set map[string]any, conditions []query.Condition) error {
	return b.DAO(ctx).Update(ctx, set, conditions)
}

func (b *Base[R]) UpdateReturning(ctx context.Context, set map[string]any, conditions []query.Condition) (*R, error) {
	return b.DAO(ctx).UpdateReturning(ctx, set, conditions)
}

func (b *Base[R]) Upsert(ctx context.Context, m map[string]any, conflictColumn string, updateColumns []string) (*R, error) {
	return b.DAO(ctx).Upsert(ctx, m, conflictColumn, updateColumns)
}

func (b *Base[R]) UpdateVersioned(ctx context.Context, set map[string]any, conditions []query.Condition, versionCol string, expected int64) (*R, error) {
	return b.DAO(ctx).UpdateVersioned(ctx, set, conditions, versionCol, expected)
}

func (b *Base[R]) SoftDelete(ctx context.Context, id string) error {
	return b.DAO(ctx).SoftDelete(ctx, id)
}

func (b *Base[R]) Restore(ctx context.Context, id string) error {
	return b.DAO(ctx).Restore(ctx, id)
}

func (b *Base[R]) HardDelete(ctx context.Context, conditions []query.Condition) error {
	return b.DAO(ctx).HardDelete(ctx, conditions)
}

// Raw escape hatches (R2 rule) — tx-propagation-aware like everything else.

func (b *Base[R]) Select(ctx context.Context, sql string, args ...any) ([]R, error) {
	return b.DAO(ctx).Select(ctx, sql, args...)
}

func (b *Base[R]) SelectOne(ctx context.Context, sql string, args ...any) (*R, error) {
	return b.DAO(ctx).SelectOne(ctx, sql, args...)
}

func (b *Base[R]) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	return b.DAO(ctx).Exec(ctx, sql, args...)
}
