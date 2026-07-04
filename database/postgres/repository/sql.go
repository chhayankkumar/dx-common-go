package repository

import "github.com/jackc/pgx/v5/pgxpool"

// BaseWithSQL is Base plus a typed accessor for a service's own sqlc-generated
// Queries type — for the residual repository that still needs sqlc alongside
// the DSL (kept for genuinely complex queries only — see database/postgres's
// three-legged persistence standard). Q is that service's concrete generated
// type (e.g. *sqlcgen.Queries) — SQL() returns it directly, so nothing about
// the generated code is hidden or wrapped; this is a thin passthrough, not an
// adapter/registry layer.
type BaseWithSQL[R any, Q any] struct {
	*Base[R]
	sql Q
}

// NewWithSQL is New plus an attached sqlc Queries value. Like WithTable/
// WithID (options.go), Q must be given explicitly at the call site since Go
// can't infer it from context:
//
//	repository.NewWithSQL[Policy, *sqlcgen.Queries](pool,
//	    sqlcgen.New(sqlcx.DB(ctx, pool)), repository.WithTable[Policy]("policy"))
func NewWithSQL[R, Q any](pool *pgxpool.Pool, sql Q, opts ...Option[R]) *BaseWithSQL[R, Q] {
	return &BaseWithSQL[R, Q]{Base: New[R](pool, opts...), sql: sql}
}

// SQL returns the attached sqlc Queries value for direct use:
// repo.SQL().GetPolicies(ctx, id).
func (b *BaseWithSQL[R, Q]) SQL() Q { return b.sql }
