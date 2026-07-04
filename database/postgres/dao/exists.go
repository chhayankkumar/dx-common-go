package dao

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Exists reports whether any row (non-deleted, when the soft-delete filter is
// on) matches conditions — the cheap alternative to Count for guard checks.
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
