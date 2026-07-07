package client

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// MultiTracer fans a single pgx.QueryTracer slot out to several tracers —
// pgxpool.Config has room for exactly one Tracer, so composing observability
// concerns (OTel spans, slow-query logging, metrics) means combining them
// into one QueryTracer rather than fighting over the slot.
type MultiTracer struct {
	tracers []pgx.QueryTracer
}

// NewMultiTracer combines tracers into one pgx.QueryTracer. Each receives
// every TraceQueryStart/TraceQueryEnd call, in the order given; the context
// returned by one tracer's TraceQueryStart is passed to the next, so a
// tracer that stores per-call state in the context (as SlowQueryTracer does)
// composes correctly with others doing the same.
func NewMultiTracer(tracers ...pgx.QueryTracer) *MultiTracer {
	return &MultiTracer{tracers: tracers}
}

// TraceQueryStart implements pgx.QueryTracer.
func (m *MultiTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, t := range m.tracers {
		ctx = t.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer.
func (m *MultiTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for _, t := range m.tracers {
		t.TraceQueryEnd(ctx, conn, data)
	}
}
