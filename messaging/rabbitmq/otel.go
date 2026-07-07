package rabbitmq

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
	"go.opentelemetry.io/otel/trace"
)

// AMQP has no official OpenTelemetry instrumentation, so this file carries the
// minimal producer/consumer span + W3C trace-context propagation the framework
// needs to stitch RabbitMQ hops into a distributed trace. Everything here reads
// OTel's *global* TracerProvider and propagator (configured by
// observability.Init) and is a no-op until one is set: the default provider
// yields no-op spans and the default propagator injects/extracts nothing, so
// the publish/consume paths pay effectively zero cost when tracing is off.

const tracerName = "github.com/datakaveri/dx-common-go/messaging/rabbitmq"

// amqpHeaderCarrier adapts an amqp.Table to the TextMapCarrier interface so the
// global propagator can read and write trace-context headers on a message.
type amqpHeaderCarrier amqp.Table

var _ propagation.TextMapCarrier = amqpHeaderCarrier(nil)

// Get returns the string value for key, or "" when absent or not a string.
func (c amqpHeaderCarrier) Get(key string) string {
	if v, ok := c[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Set stores value under key.
func (c amqpHeaderCarrier) Set(key, value string) { c[key] = value }

// Keys lists the header names currently present.
func (c amqpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// injectTraceContext writes the active span's trace context from ctx into
// headers, so a downstream consumer can continue the trace.
func injectTraceContext(ctx context.Context, headers amqp.Table) {
	otel.GetTextMapPropagator().Inject(ctx, amqpHeaderCarrier(headers))
}

// startProducerSpan begins a PRODUCER span for a publish and returns a context
// carrying it — inject the returned context's trace state into the message
// headers so the span is what a consumer links to.
func startProducerSpan(ctx context.Context, exchange, routingKey string) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, "rabbitmq.publish "+routingKey,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemRabbitmq,
			semconv.MessagingOperationTypePublish,
			semconv.MessagingDestinationName(exchange),
			semconv.MessagingRabbitmqDestinationRoutingKey(routingKey),
		),
	)
}

// startConsumerSpan extracts any trace context stamped on d by the publisher
// and begins a CONSUMER span as its child, returning a context carrying the
// span for the handler to build on.
func startConsumerSpan(ctx context.Context, d amqp.Delivery) (context.Context, trace.Span) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, amqpHeaderCarrier(d.Headers))
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemRabbitmq,
		semconv.MessagingOperationTypeReceive,
		semconv.MessagingDestinationName(d.Exchange),
		semconv.MessagingRabbitmqDestinationRoutingKey(d.RoutingKey),
	}
	if d.MessageId != "" {
		attrs = append(attrs, semconv.MessagingMessageID(d.MessageId))
	}
	return otel.Tracer(tracerName).Start(ctx, "rabbitmq.process "+d.RoutingKey,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
}

// recordSpanError marks span failed with err. A nil err leaves the span Unset
// (its default), which the exporter treats as success.
func recordSpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
