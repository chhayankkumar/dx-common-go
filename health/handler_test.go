package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveAlwaysOK(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler().Live(rec, httptest.NewRequest(http.MethodGet, "/healthz/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Live status = %d, want 200", rec.Code)
	}
	var body struct{ Status string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Status != "alive" {
		t.Fatalf("Live body = %q, want alive", body.Status)
	}
}

func TestReadyAllHealthy(t *testing.T) {
	h := NewHandler()
	h.Register("db", NewCustomChecker("db", func(context.Context) error { return nil }))
	h.Register("cache", NewCustomChecker("cache", func(context.Context) error { return nil }))

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Ready status = %d, want 200", rec.Code)
	}
	var body readyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Ready || body.Services["db"] != "healthy" {
		t.Fatalf("unexpected ready body: %+v", body)
	}
}

func TestReadyOneUnhealthyFailsClosed(t *testing.T) {
	h := NewHandler()
	h.Register("db", NewCustomChecker("db", func(context.Context) error { return nil }))
	h.Register("authz", NewCustomChecker("authz", func(context.Context) error {
		return errors.New("connection refused")
	}))

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, "/healthz/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Ready status = %d, want 503 when a dependency is down", rec.Code)
	}
	var body readyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Ready {
		t.Fatal("Ready should be false when a checker is unhealthy")
	}
	if body.Services["authz"] == "healthy" {
		t.Fatalf("failing checker reported healthy: %+v", body.Services)
	}
}

func TestHealthAggregatesStatus(t *testing.T) {
	h := NewHandler()
	h.Register("ok", NewCustomChecker("ok", func(context.Context) error { return nil }))
	h.Register("down", NewCustomChecker("down", func(context.Context) error { return errors.New("boom") }))

	rec := httptest.NewRecorder()
	h.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var status HealthStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if status.Status != "unhealthy" {
		t.Fatalf("overall status = %q, want unhealthy", status.Status)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Health status code = %d, want 503", rec.Code)
	}
	if len(status.Services) != 2 {
		t.Fatalf("expected 2 service statuses, got %d", len(status.Services))
	}
}

func TestCustomCheckerClassifiesError(t *testing.T) {
	healthy := NewCustomChecker("x", func(context.Context) error { return nil }).Check(context.Background())
	if healthy.Status != "healthy" {
		t.Fatalf("nil error → %q, want healthy", healthy.Status)
	}
	down := NewCustomChecker("x", func(context.Context) error { return errors.New("nope") }).Check(context.Background())
	if down.Status != "unhealthy" {
		t.Fatalf("error → %q, want unhealthy", down.Status)
	}
}
