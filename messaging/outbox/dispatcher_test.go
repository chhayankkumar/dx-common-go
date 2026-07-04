package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// memStore is an in-memory Store fake for testing Dispatcher without a
// database.
type memStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]Row
	sent map[uuid.UUID]bool
}

func newMemStore(rows ...Row) *memStore {
	s := &memStore{rows: map[uuid.UUID]Row{}, sent: map[uuid.UUID]bool{}}
	for _, r := range rows {
		s.rows[r.ID] = r
	}
	return s
}

func (s *memStore) Insert(_ context.Context, _ pgx.Tx, action string, payload []byte, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.New()
	s.rows[id] = Row{ID: id, Action: action, Payload: payload, RequestID: requestID}
	return nil
}

func (s *memStore) FetchUnsent(_ context.Context, limit int) ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Row
	for id, r := range s.rows {
		if s.sent[id] {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *memStore) MarkSent(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent[id] = true
	return nil
}

func (s *memStore) sentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, ok := range s.sent {
		if ok {
			n++
		}
	}
	return n
}

func TestDispatcher_DrainPublishesAndMarksSent(t *testing.T) {
	rows := []Row{{ID: uuid.New(), Action: "create"}, {ID: uuid.New(), Action: "delete"}}
	store := newMemStore(rows...)

	var published []Row
	var mu sync.Mutex
	publish := func(_ context.Context, r Row) error {
		mu.Lock()
		published = append(published, r)
		mu.Unlock()
		return nil
	}

	d := NewDispatcher(store, publish, nil, WithBatchSize(10))
	d.drain(context.Background())

	if len(published) != 2 {
		t.Fatalf("expected 2 rows published, got %d", len(published))
	}
	if store.sentCount() != 2 {
		t.Fatalf("expected 2 rows marked sent, got %d", store.sentCount())
	}
}

func TestDispatcher_PublishFailureStopsDrainWithoutMarkingSent(t *testing.T) {
	rows := []Row{{ID: uuid.New(), Action: "create"}}
	store := newMemStore(rows...)

	publish := func(_ context.Context, _ Row) error {
		return errors.New("broker down")
	}

	d := NewDispatcher(store, publish, nil)
	d.drain(context.Background())

	if store.sentCount() != 0 {
		t.Fatalf("a failed publish must not mark the row sent, got sentCount=%d", store.sentCount())
	}
}

func TestDispatcher_Job_DrainsThroughSchedulerRunner(t *testing.T) {
	rows := []Row{{ID: uuid.New(), Action: "create"}}
	store := newMemStore(rows...)

	var published int
	var mu sync.Mutex
	publish := func(_ context.Context, _ Row) error {
		mu.Lock()
		published++
		mu.Unlock()
		return nil
	}

	d := NewDispatcher(store, publish, nil, WithInterval(5*time.Millisecond))
	job := d.Job("test-outbox-dispatch")
	if job.Name != "test-outbox-dispatch" {
		t.Fatalf("Job.Name = %q, want %q", job.Name, "test-outbox-dispatch")
	}
	if job.Every != 5*time.Millisecond {
		t.Fatalf("Job.Every = %v, want the Dispatcher's configured interval", job.Every)
	}

	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Job.Run returned error: %v", err)
	}
	if store.sentCount() != 1 {
		t.Fatalf("expected 1 row marked sent via Job.Run, got %d", store.sentCount())
	}
}

func TestDispatcher_KickTriggersImmediateDrain(t *testing.T) {
	store := newMemStore(Row{ID: uuid.New(), Action: "create"})

	done := make(chan struct{}, 1)
	publish := func(_ context.Context, _ Row) error {
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}

	// Interval is deliberately long — only Kick should cause the drain
	// within the test's short timeout.
	d := NewDispatcher(store, publish, nil, WithInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	d.Kick()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Kick did not trigger a drain within 2s")
	}
}
