package client

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

// DefaultSlowQueryThreshold is used when SlowQueryTracer.Threshold is zero.
const DefaultSlowQueryThreshold = 200 * time.Millisecond

// SlowQueryTracer logs (at Warn) any query whose execution time reaches
// Threshold. It never logs argument values — only the SQL text and argument
// count — so it's safe to enable unconditionally, including against queries
// carrying sensitive parameter values.
type SlowQueryTracer struct {
	// Threshold is the minimum duration that triggers a log line. Zero means
	// DefaultSlowQueryThreshold.
	Threshold time.Duration
	// Logger receives the slow-query warnings. Nil disables logging (the
	// tracer still tracks timing but never emits a log line) — treat this as
	// "off", not a construction error, so a nil *zap.Logger is safe.
	Logger *zap.Logger
}

type slowQueryState struct {
	start    time.Time
	sql      string
	argCount int
}

type slowQueryCtxKey struct{}

// TraceQueryStart implements pgx.QueryTracer.
func (s *SlowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, slowQueryCtxKey{}, slowQueryState{
		start:    time.Now(),
		sql:      data.SQL,
		argCount: len(data.Args),
	})
}

// TraceQueryEnd implements pgx.QueryTracer.
func (s *SlowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if s.Logger == nil {
		return
	}
	st, ok := ctx.Value(slowQueryCtxKey{}).(slowQueryState)
	if !ok {
		return
	}
	threshold := s.Threshold
	if threshold <= 0 {
		threshold = DefaultSlowQueryThreshold
	}
	if d := time.Since(st.start); d >= threshold {
		s.Logger.Warn("slow query",
			zap.String("sql", st.sql),
			zap.Int("arg_count", st.argCount),
			zap.Duration("duration", d),
			zap.Error(data.Err),
		)
	}
}
