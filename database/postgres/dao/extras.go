// Plan-completion additions to BaseDAO (DB-abstraction plan: generic
// repository ops, soft-delete restore, audit auto-population): Exists,
// FindByIDs/UpdateByIDs/DeleteByIDs, Restore + configurable soft-delete
// sentinels, and WithAuditColumns + the context actor. All additions are
// opt-in and compose with WithTx/Unscoped like every other DAO method.
package dao

import (
	"context"
	"fmt"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// ── context actor (auditing) ────────────────────────────────────────────────

type actorKey struct{}

// WithActor stashes the acting principal (user id/email) on the context.
// Set it once in transport middleware; every audit-enabled DAO write then
// auto-populates its created_by/updated_by columns from it.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFromContext returns the acting principal stored by WithActor.
func ActorFromContext(ctx context.Context) (string, bool) {
	a, ok := ctx.Value(actorKey{}).(string)
	return a, ok && a != ""
}

// ── options ─────────────────────────────────────────────────────────────────

// WithSoftDeleteValues overrides the DELETED/ACTIVE sentinel pair used by the
// soft-delete filter (WithSoftDeleteFilter) and Restore — for tables whose
// lifecycle column uses different values.
func WithSoftDeleteValues[T any](deleted, active string) Option[T] {
	return func(d *BaseDAO[T]) {
		d.softDeleteDeleted, d.softDeleteActive = deleted, active
	}
}

// WithAuditColumns enables audit auto-population: on map-based writes the
// DAO fills createdBy (InsertMap/Upsert) and updatedBy (Update/
// UpdateReturning/Upsert) from the context actor (WithActor), never
// overriding a value the caller set explicitly. Pass "" to skip a column.
// created_at/updated_at stay DB-owned (DEFAULT now() / trigger).
func WithAuditColumns[T any](createdBy, updatedBy string) Option[T] {
	return func(d *BaseDAO[T]) {
		d.auditCreatedBy, d.auditUpdatedBy = createdBy, updatedBy
	}
}

func (d *BaseDAO[T]) deletedValue() string {
	if d.softDeleteDeleted != "" {
		return d.softDeleteDeleted
	}
	return "DELETED"
}

func (d *BaseDAO[T]) activeValue() string {
	if d.softDeleteActive != "" {
		return d.softDeleteActive
	}
	return "ACTIVE"
}

// auditInsert returns m plus created_by/updated_by from the context actor
// (copy-on-write; caller-provided keys win; no-op when audit is off or no
// actor is present).
func (d *BaseDAO[T]) auditInsert(ctx context.Context, m map[string]any) map[string]any {
	actor, ok := ActorFromContext(ctx)
	if !ok || (d.auditCreatedBy == "" && d.auditUpdatedBy == "") {
		return m
	}
	out := make(map[string]any, len(m)+2)
	for k, v := range m {
		out[k] = v
	}
	if d.auditCreatedBy != "" {
		if _, set := out[d.auditCreatedBy]; !set {
			out[d.auditCreatedBy] = actor
		}
	}
	if d.auditUpdatedBy != "" {
		if _, set := out[d.auditUpdatedBy]; !set {
			out[d.auditUpdatedBy] = actor
		}
	}
	return out
}

// auditUpdate returns set plus updated_by from the context actor (same
// copy-on-write rules as auditInsert).
func (d *BaseDAO[T]) auditUpdate(ctx context.Context, set map[string]any) map[string]any {
	actor, ok := ActorFromContext(ctx)
	if !ok || d.auditUpdatedBy == "" {
		return set
	}
	if _, has := set[d.auditUpdatedBy]; has {
		return set
	}
	out := make(map[string]any, len(set)+1)
	for k, v := range set {
		out[k] = v
	}
	out[d.auditUpdatedBy] = actor
	return out
}

// auditUpdateColumns ensures the updated-by column participates in an
// Upsert's DO UPDATE SET list when audit is enabled.
func (d *BaseDAO[T]) auditUpdateColumns(updateColumns []string) []string {
	if d.auditUpdatedBy == "" {
		return updateColumns
	}
	for _, c := range updateColumns {
		if c == d.auditUpdatedBy {
			return updateColumns
		}
	}
	return append(append([]string(nil), updateColumns...), d.auditUpdatedBy)
}

// ── generic repository completions ──────────────────────────────────────────

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

// FindByIDs returns all rows whose IDColumn is in ids (order not guaranteed;
// soft-delete filter applies). Empty ids → nil without touching the database.
func (d *BaseDAO[T]) FindByIDs(ctx context.Context, ids []string) ([]T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return d.FindAll(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// UpdateByIDs applies set to every row whose IDColumn is in ids (bulk update
// by key; audit auto-population applies). Empty ids is a no-op.
func (d *BaseDAO[T]) UpdateByIDs(ctx context.Context, ids []string, set map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	return d.Update(ctx, set, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// DeleteByIDs permanently deletes every row whose IDColumn is in ids (bulk
// delete by key). Empty ids is a no-op.
func (d *BaseDAO[T]) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return d.HardDelete(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}

// Restore reverses a soft delete: sets the soft-delete column back to the
// active sentinel for id. Requires WithSoftDeleteFilter; the update runs
// Unscoped (the row being restored is, by definition, currently deleted).
func (d *BaseDAO[T]) Restore(ctx context.Context, id string) error {
	if d.softDeleteColumn == "" {
		return fmt.Errorf("dao: Restore requires WithSoftDeleteFilter configuration")
	}
	return d.Unscoped().Update(ctx,
		map[string]any{d.softDeleteColumn: d.activeValue()},
		query.NewConditionBuilder().Eq(d.IDColumn, id).Build())
}
