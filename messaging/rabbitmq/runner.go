package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Delivery is re-exported so handlers need not import amqp directly.
type Delivery = amqp.Delivery

// Outcome is a handler's decision for one delivery.
type Outcome int

const (
	// Ack marks the message processed.
	Ack Outcome = iota
	// Requeue returns the message for another attempt (transient failure).
	Requeue
	// DeadLetter rejects the message without requeue (poison / unknown).
	DeadLetter
)

// Handler processes one delivery and returns an Outcome.
type Handler func(ctx context.Context, d Delivery) Outcome

// ConsumerConfig configures a ConsumerRunner.
type ConsumerConfig struct {
	URL           string
	Queue         string
	ConsumerTag   string
	PrefetchCount int
	Logger        *zap.Logger
	// Setup declares the topology (exchanges, queues, bindings) on every
	// (re)connect, before consuming begins. Required.
	Setup func(ch *amqp.Channel) error
}

// ConsumerRunner owns the dial → declare → consume → ack loop with a reconnect
// supervisor (exponential backoff, 1s→30s). It centralises the plumbing that
// dx-authz-go and dx-files-connect-api-go each hand-rolled; services supply
// only the topology Setup and a Handler returning an Outcome.
type ConsumerRunner struct {
	cfg    ConsumerConfig
	logger *zap.Logger
}

// NewConsumerRunner constructs a runner. No network IO until Run is called.
func NewConsumerRunner(cfg ConsumerConfig) *ConsumerRunner {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	return &ConsumerRunner{cfg: cfg, logger: logger}
}

// Run blocks until ctx is cancelled, reconnecting transparently on any failure.
func (r *ConsumerRunner) Run(ctx context.Context, handler Handler) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		err := r.runOnce(ctx, handler)
		if ctx.Err() != nil {
			return
		}
		r.logger.Warn("consumer dropped, will reconnect",
			zap.String("queue", r.cfg.Queue), zap.Error(err), zap.Duration("backoff", backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func (r *ConsumerRunner) runOnce(ctx context.Context, handler Handler) error {
	conn, err := amqp.Dial(r.cfg.URL)
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	if r.cfg.Setup != nil {
		if err := r.cfg.Setup(ch); err != nil {
			return fmt.Errorf("topology setup: %w", err)
		}
	}
	if r.cfg.PrefetchCount > 0 {
		if err := ch.Qos(r.cfg.PrefetchCount, 0, false); err != nil {
			return fmt.Errorf("set qos: %w", err)
		}
	}

	notifyClose := ch.NotifyClose(make(chan *amqp.Error, 1))

	deliveries, err := ch.Consume(r.cfg.Queue, r.cfg.ConsumerTag, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("start consuming: %w", err)
	}
	r.logger.Info("consumer connected", zap.String("queue", r.cfg.Queue))

	for {
		select {
		case <-ctx.Done():
			return nil
		case amqpErr := <-notifyClose:
			if amqpErr == nil {
				return errors.New("amqp channel closed cleanly")
			}
			return fmt.Errorf("amqp channel closed: %w", amqpErr)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			r.dispatch(ctx, d, handler)
		}
	}
}

func (r *ConsumerRunner) dispatch(ctx context.Context, d Delivery, handler Handler) {
	switch handler(ctx, d) {
	case Ack:
		if err := d.Ack(false); err != nil {
			r.logger.Warn("ack failed; message may be redelivered", zap.Error(err))
		}
	case Requeue:
		if err := d.Nack(false, true); err != nil {
			r.logger.Warn("nack(requeue) failed; delivery state unknown", zap.Error(err))
		}
	case DeadLetter:
		if err := d.Nack(false, false); err != nil {
			r.logger.Warn("nack(dead-letter) failed; delivery state unknown", zap.Error(err))
		}
	}
}

// Dedup is a fixed-capacity FIFO set of processed message/request IDs,
// absorbing redeliveries after a lost ack. Safe for concurrent use.
type Dedup struct {
	mu    sync.Mutex
	cap   int
	order []string
	set   map[string]struct{}
}

// NewDedup creates a Dedup retaining the most recent capacity IDs.
func NewDedup(capacity int) *Dedup {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Dedup{cap: capacity, set: make(map[string]struct{}, capacity)}
}

// Seen reports whether id was already marked. Empty ids are never seen.
func (d *Dedup) Seen(id string) bool {
	if id == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.set[id]
	return ok
}

// Mark records id, evicting the oldest entry past capacity. Empty ids are ignored.
func (d *Dedup) Mark(id string) {
	if id == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.set[id]; ok {
		return
	}
	if len(d.order) >= d.cap {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.set, oldest)
	}
	d.order = append(d.order, id)
	d.set[id] = struct{}{}
}
