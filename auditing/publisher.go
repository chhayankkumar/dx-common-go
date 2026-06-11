package auditing

import (
	"go.uber.org/zap"

	dxmq "github.com/datakaveri/dx-common-go/messaging/rabbitmq"
)

// Config wires the audit publisher. The exchange lives on the controlplane's
// internal vhost (Java default: exchange "auditing", routing key "##",
// vhost /internal).
type Config struct {
	Enabled    bool   `mapstructure:"enabled"`
	URL        string `mapstructure:"url"` // AMQP URL incl. /internal vhost
	Exchange   string `mapstructure:"exchange"`
	RoutingKey string `mapstructure:"routing_key"`
}

func (c *Config) applyDefaults() {
	if c.Exchange == "" {
		c.Exchange = "auditing"
	}
	if c.RoutingKey == "" {
		c.RoutingKey = "##"
	}
}

// Publisher sends audit records. A nil Publisher is a no-op, so callers can
// wire it unconditionally and let config decide.
type Publisher struct {
	pub        *dxmq.ReliablePublisher
	exchange   string
	routingKey string
	logger     *zap.Logger
}

// NewPublisher connects to the audit vhost. Returns (nil, nil) when disabled.
func NewPublisher(cfg Config, logger *zap.Logger) (*Publisher, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	cfg.applyDefaults()
	pub, err := dxmq.NewReliablePublisher(dxmq.PublisherConfig{
		URL:          cfg.URL,
		Exchange:     cfg.Exchange,
		ExchangeType: "topic",
		Logger:       logger,
	})
	if err != nil {
		return nil, err
	}
	return &Publisher{
		pub:        pub,
		exchange:   cfg.Exchange,
		routingKey: cfg.RoutingKey,
		logger:     logger,
	}, nil
}

// Publish is fire-and-forget: an audit failure is logged, never surfaced to
// the request path (matching the Java AuditingHandler's behaviour).
func (p *Publisher) Publish(rec *Record) {
	if p == nil || rec == nil {
		return
	}
	if err := p.pub.PublishJSON(p.exchange, p.routingKey, rec); err != nil {
		p.logger.Error("audit publish failed",
			zap.String("action", rec.Action),
			zap.String("api", rec.API),
			zap.Error(err))
	}
}

// PublishEvent sends a gateway security event on this publisher's exchange
// and routing key. The gateway constructs a dedicated Publisher pointed at
// the "gateway-logs" exchange for these.
func (p *Publisher) PublishEvent(ev *GatewayEvent) {
	if p == nil || ev == nil {
		return
	}
	if err := p.pub.PublishJSON(p.exchange, p.routingKey, ev); err != nil {
		p.logger.Error("gateway event publish failed",
			zap.String("event", ev.Event), zap.Error(err))
	}
}

// Close releases the underlying AMQP connection.
func (p *Publisher) Close() {
	if p == nil {
		return
	}
	p.pub.Close()
}
