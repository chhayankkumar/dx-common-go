package dao

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// copier is satisfied by *pgxpool.Pool and pgx.Tx (both support the binary
// COPY protocol) but not by the minimal Querier interface, so CopyFrom
// type-asserts for it rather than widening Querier for every implementer.
type copier interface {
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// CopyFrom bulk-inserts rows via PostgreSQL's binary COPY protocol — far
// faster than row-by-row INSERT for large batches, at the cost of not
// supporting RETURNING, ON CONFLICT, or triggers that only fire on INSERT.
// The underlying connection (pool or tx) must support CopyFrom; a DAO bound
// to a Querier that doesn't (e.g. a test double) returns an error.
func (d *BaseDAO[T]) CopyFrom(ctx context.Context, columns []string, rows [][]any) (int64, error) {
	cp, ok := d.DB.(copier)
	if !ok {
		return 0, fmt.Errorf("dao.CopyFrom: underlying connection does not support CopyFrom")
	}
	n, err := cp.CopyFrom(ctx, pgx.Identifier{d.TableName}, columns, pgx.CopyFromRows(rows))
	if err != nil {
		return 0, MapPgError(err)
	}
	return n, nil
}

// InsertMany inserts multiple rows in one multi-VALUES statement. Prefer
// CopyFrom for large batches; use InsertMany when the table has an
// INSERT-only trigger or the batch is small enough that COPY's setup cost
// isn't worth it.
func (d *BaseDAO[T]) InsertMany(ctx context.Context, columns []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "INSERT INTO %s (%s) VALUES ", d.TableName, strings.Join(columns, ", "))
	args := make([]any, 0, len(rows)*len(columns))
	idx := 1
	for i, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("dao.InsertMany: row %d has %d values, want %d", i, len(row), len(columns))
		}
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for j := range row {
			if j > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "$%d", idx)
			idx++
		}
		sb.WriteByte(')')
		args = append(args, row...)
	}

	if _, err := d.DB.Exec(ctx, sb.String(), args...); err != nil {
		return MapPgError(err)
	}
	return nil
}
