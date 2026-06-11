package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// PublisherConfig configures a ReliablePublisher.
type PublisherConfig struct {
	URL string
	// Exchange / ExchangeType, when set, are declared (durable) on every
	// (re)connect. Leave Exchange empty to publish only to the default
	// exchange (queue-name routing) without declaring anything.
	Exchange     string
	ExchangeType string
	Logger       *zap.Logger
}

// ReliablePublisher publishes to RabbitMQ with lazy reconnect and one
// automatic retry on a closed channel. Safe for concurrent use. It unifies
// the previously per-service publishers (dx-acl-go events.Publisher,
// dx-files-connect-api-go messaging.Client) and satisfies the
// notify/email.Publisher interface via PublishJSON.
type ReliablePublisher struct {
	cfg    PublisherConfig
	logger *zap.Logger

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewReliablePublisher dials the broker. A failed initial dial is not fatal:
// the first Publish call retries, so service startup never blocks on RMQ.
func NewReliablePublisher(cfg PublisherConfig) (*ReliablePublisher, error) {
	if cfg.URL == "" {
		return nil, errors.New("rabbitmq publisher: URL is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	p := &ReliablePublisher{cfg: cfg, logger: logger}
	if err := p.dial(); err != nil {
		logger.Warn("publisher initial dial failed; will retry on first publish", zap.Error(err))
	}
	return p, nil
}

// PublishOptions carries optional per-message metadata.
type PublishOptions struct {
	MessageID string
}

// Publish sends body to exchange/routingKey, redialing and retrying once if
// the channel was closed. An empty exchange publishes to the default exchange.
func (p *ReliablePublisher) Publish(ctx context.Context, exchange, routingKey string, body []byte, opts PublishOptions) error {
	pub := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		MessageId:    opts.MessageID,
		Body:         body,
	}

	if err := p.publishOnce(ctx, exchange, routingKey, pub); err != nil {
		if errors.Is(err, amqp.ErrClosed) || isChannelClosedErr(err) {
			p.logger.Warn("publish channel closed, redialling", zap.Error(err))
			if derr := p.dial(); derr != nil {
				return fmt.Errorf("redial after publish failure: %w", derr)
			}
			if err2 := p.publishOnce(ctx, exchange, routingKey, pub); err2 != nil {
				return fmt.Errorf("publish after redial: %w", err2)
			}
			return nil
		}
		return err
	}
	return nil
}

// PublishJSON marshals v and publishes it (background context). It implements
// the notify/email.Publisher interface so the email notifier can share this
// publisher's connection.
func (p *ReliablePublisher) PublishJSON(exchange, routingKey string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("rabbitmq publisher: marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.Publish(ctx, exchange, routingKey, body, PublishOptions{})
}

func (p *ReliablePublisher) publishOnce(ctx context.Context, exchange, routingKey string, pub amqp.Publishing) error {
	p.mu.Lock()
	ch := p.ch
	p.mu.Unlock()
	if ch == nil {
		return amqp.ErrClosed
	}
	return ch.PublishWithContext(ctx, exchange, routingKey, false, false, pub)
}

// dial atomically replaces the connection + channel and (re)declares the
// configured exchange.
func (p *ReliablePublisher) dial() error {
	conn, err := amqp.Dial(p.cfg.URL)
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}
	if p.cfg.Exchange != "" {
		kind := p.cfg.ExchangeType
		if kind == "" {
			kind = "topic"
		}
		if err := ch.ExchangeDeclare(p.cfg.Exchange, kind, true, false, false, false, nil); err != nil {
			ch.Close()
			conn.Close()
			return fmt.Errorf("declare exchange %q: %w", p.cfg.Exchange, err)
		}
	}

	p.mu.Lock()
	old := p.conn
	p.conn, p.ch = conn, ch
	p.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Close releases the connection and channel.
func (p *ReliablePublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		p.ch.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
}

// isChannelClosedErr matches amqp errors that mean the channel is unusable.
// The amqp091 driver returns plain errors for these.
func isChannelClosedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return s == "channel/connection is not open" ||
		s == `Exception (504) Reason: "channel/connection is not open"`
}
