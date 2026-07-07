package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

// Insert inserts a row using the provided column names and corresponding values.
func (d *BaseDAO[T]) Insert(ctx context.Context, columns []string, values []any) error {
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values}
	sql, args := d.builder.BuildInsert(q)

	if _, err := d.DB.Exec(ctx, sql, args...); err != nil {
		return MapPgError(err)
	}
	return nil
}

// InsertIgnore inserts a row, doing nothing if conflictColumn's value
// already exists (INSERT ... ON CONFLICT (conflictColumn) DO NOTHING) — the
// idempotent-insert pattern for a naturally-keyed row from an
// at-least-once delivery source (e.g. a message envelope's own id as the
// primary key: redelivery after a lost ack must not duplicate the row, and
// there's nothing meaningful to update on the "conflict" since it's the
// exact same record, not a real update). Returns inserted=true only when a
// new row was actually written.
func (d *BaseDAO[T]) InsertIgnore(ctx context.Context, columns []string, values []any, conflictColumn string) (inserted bool, err error) {
	if len(columns) != len(values) {
		return false, fmt.Errorf("dao.InsertIgnore: %d columns but %d values", len(columns), len(values))
	}
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING",
		d.TableName, strings.Join(columns, ", "), strings.Join(placeholders, ", "), conflictColumn)

	tag, err := d.DB.Exec(ctx, sql, values...)
	if err != nil {
		return false, MapPgError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// InsertMap inserts the non-nil fields of m (column → value, the Go
// equivalent of the Java toNonEmptyFieldsMap flow) and returns the stored
// row via RETURNING *.
func (d *BaseDAO[T]) InsertMap(ctx context.Context, m map[string]any) (*T, error) {
	m = d.auditInsert(ctx, m)
	columns, values := splitMap(m)
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values, Returning: []string{"*"}}
	sql, args := d.builder.BuildInsert(q)
	return d.selectOne(ctx, sql, args)
}

// InsertReturning inserts a row and scans the RETURNING clause into dest.
func (d *BaseDAO[T]) InsertReturning(ctx context.Context, columns []string, values []any, returning []string, dest ...any) error {
	q := query.InsertQuery{Table: d.TableName, Columns: columns, Values: values, Returning: returning}
	sql, args := d.builder.BuildInsert(q)

	if err := d.DB.QueryRow(ctx, sql, args...).Scan(dest...); err != nil {
		return fmt.Errorf("InsertReturning: %w", MapPgError(err))
	}
	return nil
}
