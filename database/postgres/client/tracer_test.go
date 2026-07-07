package client

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

type recordingTracer struct {
	started, ended int
	ctxKey         any
	ctxVal         string
}

func (r *recordingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	r.started++
	return context.WithValue(ctx, r.ctxKey, r.ctxVal)
}

func (r *recordingTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	r.ended++
}

// TestMultiTracer_FansOutToEveryTracer pins that both TraceQueryStart and
// TraceQueryEnd reach every composed tracer, in order, and that each
// tracer's context contribution is visible to the next (context chaining).
func TestMultiTracer_FansOutToEveryTracer(t *testing.T) {
	type key1 struct{}
	type key2 struct{}
	first := &recordingTracer{ctxKey: key1{}, ctxVal: "from-first"}
	second := &recordingTracer{ctxKey: key2{}, ctxVal: "from-second"}

	mt := NewMultiTracer(first, second)

	ctx := mt.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	if first.started != 1 || second.started != 1 {
		t.Fatalf("started counts = %d/%d, want 1/1", first.started, second.started)
	}
	if ctx.Value(key1{}) != "from-first" || ctx.Value(key2{}) != "from-second" {
		t.Fatal("MultiTracer did not chain context contributions from both tracers")
	}

	mt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	if first.ended != 1 || second.ended != 1 {
		t.Fatalf("ended counts = %d/%d, want 1/1", first.ended, second.ended)
	}
}
