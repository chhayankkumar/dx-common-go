package rabbitmq

import (
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// Consume starts consuming messages from queue in a background goroutine.
// For each delivery, handler is called with the message body.
// On success (nil error) the message is ack'd; on failure it is nack'd with requeue=false.
// Consume returns immediately after registering the consumer. Call Wait to block
// until the delivery channel is closed (e.g. after calling Close on the Client).
func (c *Client) Consume(queue string, handler func([]byte) error) (*ConsumerHandle, error) {
	ch := c.Channel()

	deliveries, err := ch.Consume(
		queue,
		"",    // consumer tag (auto-generated)
		false, // auto-ack — we handle ack/nack manually
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq.Consume: register consumer: %w", err)
	}

	handle := &ConsumerHandle{}
	handle.wg.Add(1)

	logger := c.cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	go func() {
		defer handle.wg.Done()
		for d := range deliveries {
			if err := handler(d.Body); err != nil {
				// Nack without requeue so poison messages don't loop forever.
				if nerr := d.Nack(false, false); nerr != nil {
					logger.Warn("rabbitmq: nack failed; delivery state unknown",
						zap.String("queue", queue), zap.Error(nerr))
				}
			} else if aerr := d.Ack(false); aerr != nil {
				logger.Warn("rabbitmq: ack failed; message may be redelivered",
					zap.String("queue", queue), zap.Error(aerr))
			}
		}
	}()

	return handle, nil
}

// ConsumerHandle tracks a running consumer goroutine.
type ConsumerHandle struct {
	wg sync.WaitGroup
}

// Wait blocks until the consumer goroutine exits.
func (h *ConsumerHandle) Wait() {
	h.wg.Wait()
}
