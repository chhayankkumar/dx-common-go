// Package sqlcx integrates sqlc-generated query packages with the platform
// data layer (the third leg of the persistence standard):
//
//	90% of persistence  → repository.Base / dao.BaseDAO + query DSL
//	                      (CRUD, dynamic filtering, paging, bulk, locking)
//	static complex SQL  → sqlc-generated queries
//	                      (multi-table JOINs, reporting, JSON aggregation,
//	                       CTEs, window functions, full-text search)
//
// sqlc generates a per-service package from checked-in .sql files
// (db/sqlc/queries/*.sql + a reference schema snapshot), giving compile-time
// typed rows with zero reflection and zero hand-written Scan code. What sqlc
// canNOT do is dynamic WHERE clauses — those stay on the DSL. Never use sqlc
// for plain CRUD/Exists/Count/paging: that is BaseDAO's job.
//
// This package contributes the one piece every generated package needs but
// sqlc does not provide: a DBTX provider that is TRANSACTION-PROPAGATION-
// AWARE, so generated queries join an ambient postgres.InTransaction exactly
// like repository.Base methods do:
//
//	// in a repository (alongside its embedded repository.Base):
//	q := sqlcgen.New(sqlcx.DB(ctx, r.Pool()))
//	rows, err := q.GetPolicyWithNames(ctx, id)
//
// Generation is reproducible without a local install:
//
//	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate
//
// Reference implementation: dx-acl-go (sqlc.yaml, db/sqlc/, and
// internal/repository/postgres/sqlcgen).
package sqlcx

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	dxpg "github.com/datakaveri/dx-common-go/database/postgres"
	"github.com/datakaveri/dx-common-go/database/postgres/dao"
)

// DB returns the ambient transaction when ctx carries one
// (postgres.InTransaction / TxFromContext), else the pool. dao.Querier is
// structurally identical to sqlc's generated DBTX interface (Exec / Query /
// QueryRow), so the return value can be passed straight to sqlcgen.New.
func DB(ctx context.Context, pool *pgxpool.Pool) dao.Querier {
	if tx, ok := dxpg.TxFromContext(ctx); ok {
		return tx
	}
	return pool
}
