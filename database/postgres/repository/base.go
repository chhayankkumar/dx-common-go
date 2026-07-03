// Package repository provides the embeddable generic base repository — the
// composition-over-inheritance counterpart of a Spring Data / Hibernate base
// repository, built on dao.BaseDAO.
//
// A service repository embeds *Base[row] and gains the full generic CRUD +
// fluent-query surface, every call transaction-propagation-aware; the service
// file then contains ONLY domain-specific methods:
//
//	type AccessRequestRepo struct {
//	    *repository.Base[requestRow]
//	}
//
//	func NewAccessRequestRepo(pool *pgxpool.Pool) *AccessRequestRepo {
//	    return &AccessRequestRepo{Base: repository.New[requestRow](
//	        pool, "request", dao.WithIDColumn[requestRow]("request_id"))}
//	}
//
//	// domain-specific queries only:
//	func (r *AccessRequestRepo) PendingExists(ctx, item, consumer) (bool, error) {
//	    return r.Query(ctx).Where(query.Eq("item_id", item), ...).Exists(ctx)
//	}
//
// Transaction behaviour: every method binds to the ambient transaction when
// the context carries one (postgres.InTransaction / TxFromContext), so a
// caller composes multi-repo atomic units with one InTransaction wrap and the
// repositories need no transaction code at all.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	dxpg "github.com/datakaveri/dx-common-go/database/postgres"
	"github.com/datakaveri/dx-common-go/database/postgres/dao"
	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// Base is the generic repository for one table with row type R. Embed a
// *Base[R] pointer in a service repository; construct with New.
type Base[R any] struct {
	pool *pgxpool.Pool
	dao  *dao.BaseDAO[R]
}

// New builds a Base for table, applying any dao options
// (dao.WithIDColumn, dao.WithSoftDeleteFilter, dao.WithAuditColumns, ...).
func New[R any](pool *pgxpool.Pool, table string, opts ...dao.Option[R]) *Base[R] {
	return &Base[R]{pool: pool, dao: dao.NewBaseDAOWith[R](pool, table, opts...)}
}

// Pool exposes the underlying pool (for InTransaction at the service layer).
func (b *Base[R]) Pool() *pgxpool.Pool { return b.pool }

// DAO returns the DAO bound to the ambient transaction when ctx carries one,
// else the pool-bound DAO — the single place the tx-propagation rule lives.
func (b *Base[R]) DAO(ctx context.Context) *dao.BaseDAO[R] {
	if tx, ok := dxpg.TxFromContext(ctx); ok {
		return b.dao.WithTx(tx)
	}
	return b.dao
}

// Unscoped returns a Base whose reads bypass the soft-delete filter.
func (b *Base[R]) Unscoped() *Base[R] {
	return &Base[R]{pool: b.pool, dao: b.dao.Unscoped()}
}

// Query starts a fluent criteria query (Where/OrderBy/Limit/Offset →
// Find/One/Count/Exists/Page), transaction-bound per ctx.
func (b *Base[R]) Query(ctx context.Context) *dao.Finder[R] {
	return b.DAO(ctx).Query()
}

// ── promoted generic CRUD (all transaction-propagation-aware) ──────────────

func (b *Base[R]) FindByID(ctx context.Context, id string) (*R, error) {
	return b.DAO(ctx).FindByID(ctx, id)
}

func (b *Base[R]) FindByIDs(ctx context.Context, ids []string) ([]R, error) {
	return b.DAO(ctx).FindByIDs(ctx, ids)
}

func (b *Base[R]) FindOne(ctx context.Context, conditions []query.Condition) (*R, error) {
	return b.DAO(ctx).FindOne(ctx, conditions)
}

func (b *Base[R]) FindAll(ctx context.Context, conditions []query.Condition) ([]R, error) {
	return b.DAO(ctx).FindAll(ctx, conditions)
}

func (b *Base[R]) FindPage(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy, limit, offset int) (*dao.Page[R], error) {
	return b.DAO(ctx).FindPage(ctx, conditions, orderBy, limit, offset)
}

func (b *Base[R]) Count(ctx context.Context, conditions []query.Condition) (int64, error) {
	return b.DAO(ctx).Count(ctx, conditions)
}

func (b *Base[R]) Exists(ctx context.Context, conditions []query.Condition) (bool, error) {
	return b.DAO(ctx).Exists(ctx, conditions)
}

func (b *Base[R]) InsertMap(ctx context.Context, m map[string]any) (*R, error) {
	return b.DAO(ctx).InsertMap(ctx, m)
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

func (b *Base[R]) UpdateByIDs(ctx context.Context, ids []string, set map[string]any) error {
	return b.DAO(ctx).UpdateByIDs(ctx, ids, set)
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

func (b *Base[R]) DeleteByIDs(ctx context.Context, ids []string) error {
	return b.DAO(ctx).DeleteByIDs(ctx, ids)
}

func (b *Base[R]) InsertMany(ctx context.Context, columns []string, rows [][]any) error {
	return b.DAO(ctx).InsertMany(ctx, columns, rows)
}

func (b *Base[R]) CopyFrom(ctx context.Context, columns []string, rows [][]any) (int64, error) {
	return b.DAO(ctx).CopyFrom(ctx, columns, rows)
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

func (b *Base[R]) FindAllOrdered(ctx context.Context, conditions []query.Condition, orderBy []query.OrderBy) ([]R, error) {
	return b.DAO(ctx).FindAllOrdered(ctx, conditions, orderBy)
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
