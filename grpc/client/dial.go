package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/datakaveri/dx-common-go/resilience"
)

const (
	defaultKeepaliveTime    = 30 * time.Second
	defaultKeepaliveTimeout = 10 * time.Second
)

type dialConfig struct {
	tracing        bool
	resilient      bool
	resilienceOpts []resilience.GRPCOption
	unary          []grpc.UnaryClientInterceptor
	extra          []grpc.DialOption
}

// Option customizes Dial. Resilience and tracing are on by default.
type Option func(*dialConfig)

// WithoutResilience disables the retry/circuit-breaker unary interceptor.
func WithoutResilience() Option { return func(c *dialConfig) { c.resilient = false } }

// WithoutTracing disables the OpenTelemetry stats handler.
func WithoutTracing() Option { return func(c *dialConfig) { c.tracing = false } }

// WithResilience tunes the resilience interceptor (policy, breaker, codes).
func WithResilience(opts ...resilience.GRPCOption) Option {
	return func(c *dialConfig) { c.resilienceOpts = append(c.resilienceOpts, opts...) }
}

// WithUnaryInterceptors appends application unary interceptors, chained after
// the resilience interceptor.
func WithUnaryInterceptors(interceptors ...grpc.UnaryClientInterceptor) Option {
	return func(c *dialConfig) { c.unary = append(c.unary, interceptors...) }
}

// WithDialOptions is an escape hatch for raw grpc.DialOptions.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *dialConfig) { c.extra = append(c.extra, opts...) }
}

// Dial creates a *grpc.ClientConn to cfg.Target with the platform defaults
// applied: transport credentials (insecure unless cfg.TLS), keepalive, an OTel
// stats handler, and a resilience unary interceptor. grpc.NewClient is lazy —
// the connection is established on first RPC — so Dial does not block on the
// server being up.
func Dial(cfg Config, opts ...Option) (*grpc.ClientConn, error) {
	if cfg.Target == "" {
		return nil, fmt.Errorf("grpc client: target is required")
	}
	dc := dialConfig{tracing: true, resilient: true}
	for _, opt := range opts {
		opt(&dc)
	}

	creds, err := transportCredentials(cfg)
	if err != nil {
		return nil, err
	}

	kaTime, kaTimeout := cfg.KeepaliveTime, cfg.KeepaliveTimeout
	if kaTime <= 0 {
		kaTime = defaultKeepaliveTime
	}
	if kaTimeout <= 0 {
		kaTimeout = defaultKeepaliveTimeout
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    kaTime,
			Timeout: kaTimeout,
		}),
	}
	if dc.tracing {
		dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}

	var unary []grpc.UnaryClientInterceptor
	if dc.resilient {
		unary = append(unary, resilience.UnaryClientInterceptor(dc.resilienceOpts...))
	}
	unary = append(unary, dc.unary...)
	if len(unary) > 0 {
		dialOpts = append(dialOpts, grpc.WithChainUnaryInterceptor(unary...))
	}
	dialOpts = append(dialOpts, dc.extra...)

	conn, err := grpc.NewClient(cfg.Target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("grpc client: dial %s: %w", cfg.Target, err)
	}
	return conn, nil
}

func transportCredentials(cfg Config) (credentials.TransportCredentials, error) {
	if !cfg.TLS {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{ServerName: cfg.ServerNameOverride, MinVersion: tls.VersionTLS12}
	if cfg.CACertPath != "" {
		pem, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("grpc client: read ca_cert_path: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("grpc client: ca_cert_path has no valid PEM certificates")
		}
		tlsCfg.RootCAs = pool
	}
	return credentials.NewTLS(tlsCfg), nil
}
