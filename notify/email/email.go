// Package email publishes notification requests to the dx-controlplane email
// verticle. The controlplane consumes the configured RabbitMQ queue (its
// `emailQueue` config, default "email-notification"), renders the template and
// sends the mail over SMTP — Go services never talk to SMTP directly.
package email

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
)

// Publisher is the minimal publishing capability the notifier needs.
// dx-common-go's messaging/rabbitmq.Client satisfies it; services with their
// own AMQP wrappers provide a one-line adapter.
type Publisher interface {
	PublishJSON(exchange, routingKey string, v any) error
}

// DefaultQueue matches the controlplane's default emailQueue config.
const DefaultQueue = "email-notification"

// Template types understood by the Java TemplateRef.Type enum.
const (
	TemplateTypePath   = "PATH"   // templateStructure is a classpath resource path
	TemplateTypeInline = "INLINE" // templateStructure is raw HTML
)

// Request mirrors org.cdpg.dx.email.model.EmailRequest. ConsumerUserID,
// TemplateType, TemplateStructure and IsCreated are required by the Java side;
// the rest are template-dependent.
type Request struct {
	ConsumerUserID    string `json:"consumerUserId"`
	TemplateType      string `json:"templateType"`      // PATH | INLINE
	TemplateStructure string `json:"templateStructure"` // resource path or raw HTML
	IsCreated         bool   `json:"isCreated"`
	ProviderUserID    string `json:"providerUserId,omitempty"`
	AssetType         string `json:"assetType,omitempty"`
	ItemID            string `json:"itemId,omitempty"`
	ShortDescription  string `json:"shortDescription,omitempty"`
	Status            string `json:"status,omitempty"`
	AssetName         string `json:"assetName,omitempty"`
	ExpiryAt          string `json:"expiryAt,omitempty"`
}

func (r Request) validate() error {
	if r.ConsumerUserID == "" {
		return errors.New("email: consumerUserId is required")
	}
	if r.TemplateType != TemplateTypePath && r.TemplateType != TemplateTypeInline {
		return fmt.Errorf("email: templateType must be %s or %s", TemplateTypePath, TemplateTypeInline)
	}
	if r.TemplateStructure == "" {
		return errors.New("email: templateStructure is required")
	}
	return nil
}

// Config controls the notifier.
type Config struct {
	// Enabled gates all sends; when false Send is a logged no-op so callers
	// never need their own flag checks.
	Enabled bool `mapstructure:"enabled"`
	// Queue is the RabbitMQ queue the controlplane email consumer reads.
	Queue string `mapstructure:"queue"`
	// TemplateType / TemplateStructure are applied to requests that don't set
	// their own, so call sites stay template-agnostic.
	TemplateType      string `mapstructure:"template_type"`
	TemplateStructure string `mapstructure:"template_structure"`
}

// Notifier publishes email requests. Sends are best-effort: a failed publish
// is logged, never propagated as a request failure.
type Notifier struct {
	cfg    Config
	client Publisher
	logger *zap.Logger
}

// NewNotifier wraps an existing publisher. logger may be nil.
func NewNotifier(cfg Config, client Publisher, logger *zap.Logger) *Notifier {
	if cfg.Queue == "" {
		cfg.Queue = DefaultQueue
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Notifier{cfg: cfg, client: client, logger: logger}
}

// Send publishes req to the email queue. Returns an error only for invalid
// requests or when the notifier is enabled and the publish fails; disabled
// notifiers return nil immediately.
func (n *Notifier) Send(ctx context.Context, req Request) error {
	if n == nil || !n.cfg.Enabled {
		return nil
	}
	if req.TemplateType == "" {
		req.TemplateType = n.cfg.TemplateType
	}
	if req.TemplateStructure == "" {
		req.TemplateStructure = n.cfg.TemplateStructure
	}
	if err := req.validate(); err != nil {
		return err
	}
	// Publish to the default exchange with the queue name as routing key —
	// the controlplane consumer reads the queue directly.
	if err := n.client.PublishJSON("", n.cfg.Queue, req); err != nil {
		n.logger.Error("email notification publish failed",
			zap.String("queue", n.cfg.Queue),
			zap.String("consumerUserId", req.ConsumerUserID),
			zap.Error(err))
		return fmt.Errorf("email: publish: %w", err)
	}
	return nil
}
