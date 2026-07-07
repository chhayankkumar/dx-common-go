package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// ── CORS ─────────────────────────────────────────────────────────────────────

func TestCORS_PreflightShortCircuits(t *testing.T) {
	called := false
	h := CORS(DefaultCORSConfig())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	r := httptest.NewRequest(http.MethodOptions, "/x", nil)
	r.Header.Set("Origin", "http://app")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight code = %d", w.Code)
	}
	if called {
		t.Fatal("preflight must not call next handler")
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("missing allow-methods")
	}
}

func TestCORS_OriginAllowlist(t *testing.T) {
	cfg := CORSConfig{AllowedOrigins: []string{"http://good"}, AllowedMethods: []string{"GET"}}
	h := CORS(cfg)(http.HandlerFunc(ok))

	// Allowed origin reflected.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "http://good")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://good" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("reflecting a specific origin must set Vary: Origin, got %q", got)
	}

	// Disallowed origin not reflected.
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2.Header.Set("Origin", "http://evil")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("evil origin should not be reflected, got %q", got)
	}
}

// ── RequestID ────────────────────────────────────────────────────────────────

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var ctxID string
	h := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromCtx(r.Context())
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if ctxID == "" {
		t.Fatal("expected generated request id in context")
	}
	if w.Header().Get("X-Request-ID") != ctxID {
		t.Fatalf("response header %q != ctx %q", w.Header().Get("X-Request-ID"), ctxID)
	}
}

func TestRequestID_PreservesInbound(t *testing.T) {
	const id = "abc-123"
	var ctxID string
	h := RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctxID = RequestIDFromCtx(r.Context())
	}))
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Request-ID", id)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if ctxID != id || w.Header().Get("X-Request-ID") != id {
		t.Fatalf("inbound id not preserved: ctx=%q hdr=%q", ctxID, w.Header().Get("X-Request-ID"))
	}
}

// ── Recovery ─────────────────────────────────────────────────────────────────

func TestRecovery_TurnsPanicInto500(t *testing.T) {
	h := Recovery(zap.NewNop())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// ── MaxUploadSize ────────────────────────────────────────────────────────────

func TestMaxUploadSize_RejectsOversizedBody(t *testing.T) {
	// Handler tries to read the whole body; MaxBytesReader caps it.
	h := MaxUploadSize(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too big", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("0123456789ABCDEF"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", w.Code)
	}
}
