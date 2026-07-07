package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSlowQueryTracer_LogsWhenAtOrOverThreshold(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	tr := &SlowQueryTracer{Threshold: 1, Logger: zap.New(core)} // 1ns: any real duration qualifies

	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM policy WHERE _id = $1",
		Args: []any{"secret-looking-value"},
	})
	time.Sleep(time.Millisecond)
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["sql"] != "SELECT * FROM policy WHERE _id = $1" {
		t.Fatalf("sql field = %v", fields["sql"])
	}
	if fields["arg_count"] != int64(1) {
		t.Fatalf("arg_count field = %v, want 1", fields["arg_count"])
	}
	for k, v := range fields {
		if s, ok := v.(string); ok && s == "secret-looking-value" {
			t.Fatalf("argument value leaked into log field %q", k)
		}
	}
	if !strings.Contains(entries[0].Message, "slow query") {
		t.Fatalf("unexpected log message: %q", entries[0].Message)
	}
}

func TestSlowQueryTracer_SilentUnderThreshold(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	tr := &SlowQueryTracer{Threshold: time.Hour, Logger: zap.New(core)}

	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if len(logs.All()) != 0 {
		t.Fatalf("got %d log entries, want 0 (under threshold)", len(logs.All()))
	}
}

func TestSlowQueryTracer_NilLoggerIsSafe(t *testing.T) {
	tr := &SlowQueryTracer{Threshold: 1}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{}) // must not panic
}
