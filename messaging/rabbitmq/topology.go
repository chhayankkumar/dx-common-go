package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DeclareExchange declares a named exchange on the broker.
// kind must be one of "direct", "fanout", "topic", or "headers".
func (c *Client) DeclareExchange(name, kind string, durable bool) error {
	ch := c.Channel()
	err := ch.ExchangeDeclare(
		name,
		kind,
		durable,
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("rabbitmq.DeclareExchange %q: %w", name, err)
	}
	return nil
}

// DeclareQueue declares a named queue on the broker and returns the Queue descriptor.
func (c *Client) DeclareQueue(name string, durable bool) (amqp.Queue, error) {
	return c.DeclareQueueWithArgs(name, durable, nil)
}

// DeclareQueueWithArgs declares a queue with x-arguments (e.g. a
// dead-letter-exchange or message TTL). args may be nil. Using map[string]any
// keeps callers free of the amqp091 import.
func (c *Client) DeclareQueueWithArgs(name string, durable bool, args map[string]any) (amqp.Queue, error) {
	ch := c.Channel()
	q, err := ch.QueueDeclare(
		name,
		durable,
		false, // autoDelete
		false, // exclusive
		false, // noWait
		amqp.Table(args),
	)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueue %q: %w", name, err)
	}
	return q, nil
}

// DeleteQueue removes a queue (force delete; not ifUnused/ifEmpty).
func (c *Client) DeleteQueue(name string) error {
	ch := c.Channel()
	if _, err := ch.QueueDelete(name, false, false, false); err != nil {
		return fmt.Errorf("rabbitmq.DeleteQueue %q: %w", name, err)
	}
	return nil
}

// BindQueue binds a queue to an exchange using the supplied routing key.
func (c *Client) BindQueue(queue, exchange, routingKey string) error {
	ch := c.Channel()
	err := ch.QueueBind(
		queue,
		routingKey,
		exchange,
		false, // noWait
		nil,   // args
	)
	if err != nil {
		return fmt.Errorf("rabbitmq.BindQueue %q→%q (key=%q): %w", queue, exchange, routingKey, err)
	}
	return nil
}

// DeclareQueueWithDLQ idempotently declares queue (bound to exchange with
// bindingKey) plus a dead-letter exchange/queue pair for messages
// nacked-without-requeue (ConsumerRunner's DeadLetter outcome, or
// MaxAttempts exhaustion). This lifts the pattern hand-rolled independently
// in dx-audit-go/dx-notification-go's declareTopology.
//
// The dead-letter exchange must be a topic exchange, not direct: a
// dead-lettered message keeps its original routing key (unless
// x-dead-letter-routing-key overrides it), and the main queue is typically
// bound with a wildcard, so a direct DLX would silently drop everything — a
// literal "#" binding key only means "match all" on a topic exchange.
//
// Callers running this from inside a ConsumerRunner's Setup (which supplies
// a raw *amqp.Channel, not a *Client) should call the package-level
// DeclareQueueWithDLQ instead; this method is a thin wrapper around it using
// the Client's own channel.
func (c *Client) DeclareQueueWithDLQ(exchange, exchangeKind, queue, bindingKey string, durable bool) (amqp.Queue, error) {
	return DeclareQueueWithDLQ(c.Channel(), exchange, exchangeKind, queue, bindingKey, durable)
}

// DeclareQueueWithDLQ is the free-function form, usable directly inside a
// ConsumerRunner's Setup callback. See the Client method doc for the
// topic-vs-direct DLX rationale.
func DeclareQueueWithDLQ(ch *amqp.Channel, exchange, exchangeKind, queue, bindingKey string, durable bool) (amqp.Queue, error) {
	if err := ch.ExchangeDeclare(exchange, exchangeKind, durable, false, false, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: declare exchange %q: %w", exchange, err)
	}

	dlx := exchange + ".dlx"
	if err := ch.ExchangeDeclare(dlx, "topic", durable, false, false, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: declare dlx %q: %w", dlx, err)
	}
	dlq := queue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, durable, false, false, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: declare dlq %q: %w", dlq, err)
	}
	if err := ch.QueueBind(dlq, "#", dlx, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: bind dlq %q: %w", dlq, err)
	}

	args := amqp.Table{"x-dead-letter-exchange": dlx}
	q, err := ch.QueueDeclare(queue, durable, false, false, false, args)
	if err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: declare queue %q: %w", queue, err)
	}
	if err := ch.QueueBind(queue, bindingKey, exchange, false, nil); err != nil {
		return amqp.Queue{}, fmt.Errorf("rabbitmq.DeclareQueueWithDLQ: bind queue %q: %w", queue, err)
	}
	return q, nil
}
