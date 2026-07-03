package outbox

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// Publish delivers one outbox row to the broker. Returning an error is
// interpreted as "the broker is likely unavailable" — the dispatcher stops
// draining and waits for the next tick/Kick rather than hot-looping on the
// same row, and does NOT mark the row sent. Returning nil marks the row
// sent (the dispatcher calls Store.MarkSent itself right after a nil
// return) — so a row whose payload can never be published (e.g. it fails to
// unmarshal) should be logged loudly by the caller and then have Publish
// return nil to skip it, exactly as for a successful publish; there is no
// need to call Store.MarkSent from inside Publish as well.
type Publish func(ctx context.Context, row Row) error

// Dispatcher drains a Store to a broker via Publish. It ticks on an
// interval and can be Kick()ed for low-latency delivery right after a write.
type Dispatcher struct {
	store    Store
	publish  Publish
	logger   *zap.Logger
	interval time.Duration
	batch    int
	kick     chan struct{}
}

// DispatcherOption configures a Dispatcher at construction time.
type DispatcherOption func(*Dispatcher)

// WithInterval overrides the default 5s poll interval.
func WithInterval(d time.Duration) DispatcherOption {
	return func(disp *Dispatcher) { disp.interval = d }
}

// WithBatchSize overrides the default batch size of 100.
func WithBatchSize(n int) DispatcherOption {
	return func(disp *Dispatcher) { disp.batch = n }
}

// NewDispatcher constructs a Dispatcher. No network/DB IO happens until Run.
func NewDispatcher(store Store, publish Publish, logger *zap.Logger, opts ...DispatcherOption) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	d := &Dispatcher{
		store:    store,
		publish:  publish,
		logger:   logger,
		interval: 5 * time.Second,
		batch:    100,
		kick:     make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Kick requests an immediate drain (non-blocking; coalesces with any
// already-pending kick). Safe to call on a nil *Dispatcher (no-op), so
// callers that construct the dispatcher conditionally need not nil-check.
func (d *Dispatcher) Kick() {
	if d == nil {
		return
	}
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled, draining the store on every tick/kick.
func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.kick:
		}
		d.drain(ctx)
	}
}

func (d *Dispatcher) drain(ctx context.Context) {
	for {
		rows, err := d.store.FetchUnsent(ctx, d.batch)
		if err != nil {
			d.logger.Error("outbox: fetch unsent failed", zap.Error(err))
			return
		}
		if len(rows) == 0 {
			return
		}
		for _, row := range rows {
			if err := d.publish(ctx, row); err != nil {
				d.logger.Warn("outbox: publish failed; will retry",
					zap.String("id", row.ID.String()),
					zap.Int("attempts", row.Attempts),
					zap.Error(err))
				return // broker likely down — back off until next tick
			}
			if err := d.store.MarkSent(ctx, row.ID); err != nil {
				// Published but not marked: the event may be re-published on
				// retry. Callers should make delivery idempotent downstream
				// (dedup by request_id, as dx-acl-go's consumer already does).
				d.logger.Error("outbox: mark-sent failed", zap.String("id", row.ID.String()), zap.Error(err))
				return
			}
		}
		if len(rows) < d.batch {
			return
		}
	}
}
