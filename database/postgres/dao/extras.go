// Package-level audit/actor support: WithActor stashes the acting principal
// on a context; WithAuditColumns (an Option) and the auditInsert/auditUpdate
// helpers below use it to auto-populate created_by/updated_by on map-based
// writes (Insert.go/update.go). WithSoftDeleteValues configures the
// DELETED/ACTIVE sentinel pair the soft-delete filter and delete.go's
// Restore use. FindByIDs is the one bulk-read helper without a more specific
// topic file to live in.

package dao

import (
	"context"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
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

// FindByIDs returns all rows whose IDColumn is in ids (order not guaranteed;
// soft-delete filter applies). Empty ids → nil without touching the database.
func (d *BaseDAO[T]) FindByIDs(ctx context.Context, ids []string) ([]T, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return d.FindAll(ctx, query.NewConditionBuilder().In(d.IDColumn, ids).Build())
}
