package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Row is one pending (or already-sent) outbox event.
type Row struct {
	ID        uuid.UUID
	Action    string
	Payload   []byte
	RequestID string
	CreatedAt time.Time
	Attempts  int
}

// Store persists and retrieves outbox rows. PGStore is the production
// implementation; tests may supply their own.
type Store interface {
	// Insert writes one event row on tx, so it commits or rolls back with
	// the caller's domain write.
	Insert(ctx context.Context, tx pgx.Tx, action string, payload []byte, requestID string) error
	// FetchUnsent atomically claims up to limit unsent rows (SKIP LOCKED,
	// bumping their attempt counters), safe for concurrent dispatcher
	// instances.
	FetchUnsent(ctx context.Context, limit int) ([]Row, error)
	// MarkSent stamps a row as published.
	MarkSent(ctx context.Context, id uuid.UUID) error
}

// PGStore is a Store backed by a single Postgres table matching the shape
// documented in doc.go. table must be a trusted, compile-time-constant
// identifier — it is interpolated into SQL (table names can't be bind
// parameters), the same convention dao.BaseDAO.TableName uses. Never derive
// it from user input.
type PGStore struct {
	pool  *pgxpool.Pool
	table string
}

// NewPGStore constructs a PGStore for table (already created by the
// caller's own migration).
func NewPGStore(pool *pgxpool.Pool, table string) *PGStore {
	return &PGStore{pool: pool, table: table}
}

func (s *PGStore) Insert(ctx context.Context, tx pgx.Tx, action string, payload []byte, requestID string) error {
	sql := fmt.Sprintf(`INSERT INTO %s (id, action, payload, request_id) VALUES ($1, $2, $3, $4)`, s.table)
	if _, err := tx.Exec(ctx, sql, uuid.New(), action, payload, requestID); err != nil {
		return fmt.Errorf("outbox: insert: %w", err)
	}
	return nil
}

func (s *PGStore) FetchUnsent(ctx context.Context, limit int) ([]Row, error) {
	sql := fmt.Sprintf(`
		WITH claimed AS (
			SELECT id FROM %s
			 WHERE sent_at IS NULL
			 ORDER BY created_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE %s o
		   SET attempts = o.attempts + 1
		  FROM claimed c
		 WHERE o.id = c.id
		RETURNING o.id, o.action, o.payload, o.request_id, o.created_at, o.attempts
	`, s.table, s.table)

	rows, err := s.pool.Query(ctx, sql, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: fetch unsent: %w", err)
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.ID, &r.Action, &r.Payload, &r.RequestID, &r.CreatedAt, &r.Attempts); err != nil {
			return nil, fmt.Errorf("outbox: scan row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PGStore) MarkSent(ctx context.Context, id uuid.UUID) error {
	sql := fmt.Sprintf(`UPDATE %s SET sent_at = NOW() WHERE id = $1`, s.table)
	if _, err := s.pool.Exec(ctx, sql, id); err != nil {
		return fmt.Errorf("outbox: mark sent: %w", err)
	}
	return nil
}
