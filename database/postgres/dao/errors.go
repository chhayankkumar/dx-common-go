package dao

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// PostgreSQL error codes of interest.
const (
	pgErrUniqueViolation     = "23505"
	pgErrForeignKeyViolation = "23503"
	pgErrNotNullViolation    = "23502"
	pgErrCheckViolation      = "23514"
	pgErrSerialization       = "40001"
	pgErrDeadlock            = "40P01"
)

// ErrStaleVersion is returned by BaseDAO.UpdateVersioned when the row's
// current version no longer matches the caller's expected value (either it
// was concurrently modified, or it doesn't exist).
var ErrStaleVersion = dxerrors.NewConflict("resource was modified by another update (stale version)")

// MapPgError translates low-level pgx / pgconn errors to DxError types.
// It is safe to call with a nil error (returns nil).
func MapPgError(err error) error {
	if err == nil {
		return nil
	}

	// No rows — return NotFound.
	if errors.Is(err, pgx.ErrNoRows) {
		return dxerrors.NewNotFound("resource not found")
	}

	// Inspect PostgreSQL error code.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgErrUniqueViolation:
			return dxerrors.NewConflict("resource already exists: " + pgErr.Detail)
		case pgErrForeignKeyViolation:
			return dxerrors.NewValidation("foreign key constraint violated: " + pgErr.Detail)
		case pgErrNotNullViolation:
			return dxerrors.NewValidation("required field is null: " + pgErr.ColumnName)
		case pgErrCheckViolation:
			return dxerrors.NewValidation("check constraint violated: " + pgErr.ConstraintName)
		case pgErrSerialization:
			return dxerrors.NewDatabase("serialization failure, please retry")
		case pgErrDeadlock:
			return dxerrors.NewDatabase("deadlock detected, please retry")
		}
	}

	return fmt.Errorf("database error: %w", err)
}
