package dao

import (
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// ErrStaleVersion is returned by BaseDAO.UpdateVersioned when the row's
// current version no longer matches the caller's expected value (either it
// was concurrently modified, or it doesn't exist).
var ErrStaleVersion = dxerrors.NewConflict("resource was modified by another update (stale version)")

// MapPgError translates low-level pgx / pgconn errors to DxError types.
// It is safe to call with a nil error (returns nil).
//
// It delegates to dxerrors.MapPostgresError — the single source of truth for
// pg-code → DxError translation, shared with errors.HandleDatabaseError so
// the same database failure always yields the same client-visible status
// (unique→409, FK/not-null/check→400, serialization/deadlock→500, no-rows→404).
func MapPgError(err error) error {
	return dxerrors.MapPostgresError(err)
}
