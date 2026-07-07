package httpserver

import (
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Port != 8080 || cfg.ReadTimeout != 15*time.Second ||
		cfg.WriteTimeout != 30*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected DefaultConfig: %+v", cfg)
	}
}

func TestStartTLSRequiresCerts(t *testing.T) {
	s := New(DefaultConfig(), http.NewServeMux(), zap.NewNop())
	if err := s.StartTLS(); err == nil {
		t.Fatal("StartTLS without cert/key files should error")
	}
}

// TestServeAndGracefulShutdown starts the wrapped server on a free port, serves
// a request, then shuts down gracefully — exercising New's wiring and the
// shutdown path without depending on process signals.
func TestServeAndGracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := DefaultConfig()
	cfg.Port = port
	var hits int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	})
	s := New(cfg, h, zap.NewNop())

	go func() { _ = s.httpServer.ListenAndServe() }()
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	// Wait for the listener to come up.
	deadline := time.Now().Add(2 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		if resp, err = http.Get(url); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || atomic.LoadInt32(&hits) == 0 {
		t.Fatalf("request not served: status=%d hits=%d", resp.StatusCode, hits)
	}

	if err := s.shutdown(); err != nil {
		t.Fatalf("graceful shutdown: %v", err)
	}
	// After shutdown the port is closed.
	if _, err := http.Get(url); err == nil {
		t.Fatal("server should be down after shutdown")
	}
}
