package dao

import (
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// MapPgError translates low-level pgx / pgconn errors to DxError types.
// It is safe to call with a nil error (returns nil).
//
// It delegates to dxerrors.MapPostgresError — the single source of truth for
// pg-code → DxError translation, shared with errors.HandleDatabaseError so
// the same database failure always yields the same client-visible status.
func MapPgError(err error) error {
	return dxerrors.MapPostgresError(err)
}
