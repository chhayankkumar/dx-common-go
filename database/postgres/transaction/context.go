// Package transaction provides transaction management and propagation for
// PostgreSQL: WithTransaction/InTransaction begin/commit/rollback (the
// latter propagating an ambient transaction through context so repositories
// several calls deep join it automatically), WithRetryableTx/
// InRetryableTransaction retry on serialization failure/deadlock, and
// WithAdvisoryLock provides session-level advisory locking. Connection
// pooling lives in the sibling database/postgres/client package.
package transaction

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// txContextKey is unexported so only this package can stash/retrieve the
// ambient transaction — callers only ever see it through TxFromContext.
type txContextKey struct{}

// TxFromContext returns the transaction InTransaction stashed on ctx, if any.
// Repositories use this to bind to an ambient transaction when present and
// fall back to the pool otherwise, so a caller several layers up can compose
// multiple repository calls into one atomic unit without threading a pgx.Tx
// through every signature.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(pgx.Tx)
	return tx, ok
}
