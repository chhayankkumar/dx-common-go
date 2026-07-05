package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func fastGRPCPolicy(attempts int) GRPCOption {
	return WithGRPCPolicy(NewPolicy(WithMaxAttempts(attempts), WithBaseDelay(time.Millisecond), WithJitter(false)))
}

func TestGRPCRetriesTransientCode(t *testing.T) {
	calls := 0
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		calls++
		if calls < 3 {
			return status.Error(codes.Unavailable, "try later")
		}
		return nil
	}
	ic := UnaryClientInterceptor(fastGRPCPolicy(3))
	if err := ic(context.Background(), "/svc/M", nil, nil, nil, invoker); err != nil {
		t.Fatalf("interceptor = %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("invocations = %d, want 3", calls)
	}
}

func TestGRPCDoesNotRetryPermanentCode(t *testing.T) {
	calls := 0
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		calls++
		return status.Error(codes.InvalidArgument, "bad")
	}
	ic := UnaryClientInterceptor(fastGRPCPolicy(3))
	err := ic(context.Background(), "/svc/M", nil, nil, nil, invoker)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want InvalidArgument", err)
	}
	if calls != 1 {
		t.Fatalf("invocations = %d, want 1 (permanent code not retried)", calls)
	}
}

func TestGRPCBreakerTrips(t *testing.T) {
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return status.Error(codes.Unavailable, "down")
	}
	breaker := NewCircuitBreaker(WithFailureThreshold(2), WithCooldown(time.Minute))
	ic := UnaryClientInterceptor(fastGRPCPolicy(2), WithGRPCBreaker(breaker))

	_ = ic(context.Background(), "/svc/M", nil, nil, nil, invoker) // 2 attempts → 2 failures → trips
	if breaker.State() != StateOpen {
		t.Fatalf("breaker state = %v, want open", breaker.State())
	}
	// Next call rejected by the open breaker.
	err := ic(context.Background(), "/svc/M", nil, nil, nil, invoker)
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("open-breaker call = %v, want ErrOpen", err)
	}
}
