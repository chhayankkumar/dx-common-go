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
//	    // WithTable/WithID need the explicit [requestRow] type argument here —
//	    // Go can't infer it from New's own type parameter across the nested
//	    // call (no bidirectional/expected-type inference in Go generics).
//	    return &AccessRequestRepo{Base: repository.New[requestRow](pool,
//	        repository.WithTable[requestRow]("request"),
//	        repository.WithID[requestRow]("request_id"))}
//	}
//
//	// Skip WithTable/WithID entirely if requestRow implements
//	// dao.TableDescriber (TableName()/IDColumn() methods) — then
//	// repository.New[requestRow](pool) alone is enough.
//
//	// domain-specific queries only:
//	func (r *AccessRequestRepo) PendingExists(ctx, item, consumer) (bool, error) {
//	    return r.Query(ctx).Where(query.Eq("item_id", item), ...).Exists(ctx)
//	}
//
// Transaction behaviour: every method binds to the ambient transaction when
// the context carries one (transaction.InTransaction / TxFromContext), so a
// caller composes multi-repo atomic units with one InTransaction wrap and the
// repositories need no transaction code at all.
//
// This file holds Base's struct/constructors; promoted CRUD/paging/batch
// passthroughs live in their own topic files (crud.go, paging.go, batch.go)
// within this same package.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/datakaveri/dx-common-go/database/postgres/dao"
	"github.com/datakaveri/dx-common-go/database/postgres/transaction"
)

// Base is the generic repository for one table with row type R. Embed a
// *Base[R] pointer in a service repository; construct with New.
type Base[R any] struct {
	pool *pgxpool.Pool
	dao  *dao.BaseDAO[R]
}

// New builds a Base from Options. Table name comes from WithTable, or (if
// omitted) from R implementing dao.TableDescriber — panics if neither is
// available, same rationale as dao.NewBaseDAOFromEntity's panic.
func New[R any](pool *pgxpool.Pool, opts ...Option[R]) *Base[R] {
	cfg := &config[R]{}
	for _, opt := range opts {
		opt(cfg)
	}
	daoOpts := cfg.daoOpts
	if cfg.idCol != "" {
		daoOpts = append(daoOpts, dao.WithIDColumn[R](cfg.idCol))
	}

	var d *dao.BaseDAO[R]
	if cfg.table != "" {
		d = dao.NewBaseDAOWith[R](pool, cfg.table, daoOpts...)
	} else {
		d = dao.NewBaseDAOFromEntity[R](pool, daoOpts...)
	}
	return &Base[R]{pool: pool, dao: d}
}

// Pool exposes the underlying pool (for InTransaction at the service layer).
func (b *Base[R]) Pool() *pgxpool.Pool { return b.pool }

// DAO returns the DAO bound to the ambient transaction when ctx carries one,
// else the pool-bound DAO — the single place the tx-propagation rule lives.
func (b *Base[R]) DAO(ctx context.Context) *dao.BaseDAO[R] {
	if tx, ok := transaction.TxFromContext(ctx); ok {
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
