package rabbitmq

import (
	"context"
	"sort"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestAMQPHeaderCarrier(t *testing.T) {
	t.Run("get returns string values and ignores non-strings", func(t *testing.T) {
		c := amqpHeaderCarrier(amqp.Table{
			"traceparent": "00-abc-def-01",
			"x-int":       42,
		})
		assert.Equal(t, "00-abc-def-01", c.Get("traceparent"))
		assert.Empty(t, c.Get("x-int"), "non-string header must read as empty")
		assert.Empty(t, c.Get("absent"))
	})

	t.Run("set writes into the backing table", func(t *testing.T) {
		table := amqp.Table{}
		amqpHeaderCarrier(table).Set("traceparent", "00-abc-def-01")
		assert.Equal(t, "00-abc-def-01", table["traceparent"])
	})

	t.Run("keys lists present headers", func(t *testing.T) {
		c := amqpHeaderCarrier(amqp.Table{"a": "1", "b": "2"})
		keys := c.Keys()
		sort.Strings(keys)
		assert.Equal(t, []string{"a", "b"}, keys)
	})
}

// TestInjectExtractRoundTrip proves the producer→consumer trace linkage: a span
// created on the publish side, injected into headers, and extracted on the
// consume side yields the same trace ID with the consumer span as its child.
func TestInjectExtractRoundTrip(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// Producer side: start a span, inject its context into message headers.
	pctx, pspan := tp.Tracer("test").Start(context.Background(), "publish")
	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(pctx, amqpHeaderCarrier(headers))
	pspan.End()

	require.Contains(t, headers, "traceparent", "propagator must stamp W3C traceparent")

	// Consumer side: extract from the delivery, start a child span.
	d := amqp.Delivery{Headers: headers}
	cctx := otel.GetTextMapPropagator().Extract(context.Background(), amqpHeaderCarrier(d.Headers))
	_, cspan := tp.Tracer("test").Start(cctx, "consume")
	defer cspan.End()

	producerTID := pspan.SpanContext().TraceID()
	consumerSC := trace.SpanContextFromContext(cctx)
	assert.Equal(t, producerTID, consumerSC.TraceID(), "consumer must join the producer trace")
	assert.Equal(t, producerTID, cspan.SpanContext().TraceID())
}

func TestStartSpansAreNoOpByDefault(t *testing.T) {
	// With no SDK TracerProvider registered, the global tracer yields
	// non-recording spans — the publish/consume hot paths must not depend on
	// an SDK being present.
	_, pspan := startProducerSpan(context.Background(), "authz", "policy.created")
	assert.False(t, pspan.IsRecording(), "no-op span must not record")
	pspan.End()

	d := amqp.Delivery{Exchange: "authz", RoutingKey: "policy.created", MessageId: "m1"}
	_, cspan := startConsumerSpan(context.Background(), d)
	assert.False(t, cspan.IsRecording())
	cspan.End()
}

func TestInjectTraceContextNoPropagatorIsInert(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })

	headers := amqp.Table{}
	injectTraceContext(context.Background(), headers)
	assert.Empty(t, headers, "an empty propagator must write no headers")
}
