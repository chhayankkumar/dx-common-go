package resilience

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// defaultRetryableCodes are the transient gRPC status codes worth retrying.
var defaultRetryableCodes = map[codes.Code]bool{
	codes.Unavailable:       true,
	codes.ResourceExhausted: true,
	codes.Aborted:           true,
}

type grpcConfig struct {
	policy      Policy
	breaker     *CircuitBreaker
	isRetryable func(codes.Code) bool
	onRetry     func(attempt int, method string, err error)
}

// GRPCOption customizes UnaryClientInterceptor.
type GRPCOption func(*grpcConfig)

// WithGRPCPolicy sets the retry policy (default: DefaultPolicy).
func WithGRPCPolicy(p Policy) GRPCOption { return func(c *grpcConfig) { c.policy = p } }

// WithGRPCBreaker attaches a circuit breaker shared across calls.
func WithGRPCBreaker(b *CircuitBreaker) GRPCOption { return func(c *grpcConfig) { c.breaker = b } }

// WithRetryableCodes overrides which gRPC codes are retried.
func WithRetryableCodes(cs ...codes.Code) GRPCOption {
	return func(c *grpcConfig) {
		m := make(map[codes.Code]bool, len(cs))
		for _, x := range cs {
			m[x] = true
		}
		c.isRetryable = func(code codes.Code) bool { return m[code] }
	}
}

// WithGRPCOnRetry registers a per-retry hook (metrics/logging seam).
func WithGRPCOnRetry(f func(attempt int, method string, err error)) GRPCOption {
	return func(c *grpcConfig) { c.onRetry = f }
}

// UnaryClientInterceptor returns a grpc.UnaryClientInterceptor that retries
// transient failures per a Policy and, when configured, trips a CircuitBreaker.
// Only idempotent-safe transient codes (Unavailable/ResourceExhausted/Aborted)
// are retried by default — override with WithRetryableCodes.
func UnaryClientInterceptor(opts ...GRPCOption) grpc.UnaryClientInterceptor {
	cfg := &grpcConfig{
		policy:      DefaultPolicy(),
		isRetryable: func(code codes.Code) bool { return defaultRetryableCodes[code] },
	}
	for _, o := range opts {
		o(cfg)
	}

	retryable := func(err error) bool {
		if err == nil {
			return false
		}
		return cfg.isRetryable(status.Code(err))
	}

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
		call := func(ctx context.Context) error {
			if cfg.breaker == nil {
				return invoker(ctx, method, req, reply, cc, callOpts...)
			}
			return cfg.breaker.Execute(func() error {
				return invoker(ctx, method, req, reply, cc, callOpts...)
			})
		}
		return Retry(ctx, cfg.policy, call,
			WithRetryable(retryable),
			WithOnRetry(func(attempt int, err error, _ time.Duration) {
				if cfg.onRetry != nil {
					cfg.onRetry(attempt, method, err)
				}
			}),
		)
	}
}
