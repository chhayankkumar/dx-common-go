package appid

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenSource_FetchesAndCaches(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("expected client_credentials grant, got %q", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","expires_in":300}`))
	}))
	defer srv.Close()

	ts := newTokenSource(Config{
		TokenURL: srv.URL, ClientID: "svc", ClientSecret: "sec", Scope: "grpc:controlplane",
	})

	tok, err := ts.Token(context.Background())
	if err != nil || tok != "tok-123" {
		t.Fatalf("first token: %q err=%v", tok, err)
	}
	// Second call within validity must be served from cache (no new HTTP hit).
	tok2, err := ts.Token(context.Background())
	if err != nil || tok2 != "tok-123" {
		t.Fatalf("cached token: %q err=%v", tok2, err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 token fetch, got %d", got)
	}
}

func TestTokenSource_RefreshesNearExpiry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		// expires_in below the 30s refresh threshold → never cached.
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":10}`))
	}))
	defer srv.Close()

	ts := newTokenSource(Config{TokenURL: srv.URL, ClientID: "svc", ClientSecret: "sec"})
	for i := 0; i < 3; i++ {
		if _, err := ts.Token(context.Background()); err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("near-expiry tokens should not cache; expected 3 fetches, got %d", got)
	}
}

func TestTokenSource_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ts := newTokenSource(Config{TokenURL: srv.URL, ClientID: "svc", ClientSecret: "bad"})
	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestBasicCredentials(t *testing.T) {
	mk := func(v string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if v != "" {
			r.Header.Set("Authorization", v)
		}
		return r
	}
	good := "Basic " + base64.StdEncoding.EncodeToString([]byte("app-1:secret-9"))

	id, secret, ok := basicCredentials(mk(good))
	if !ok || id != "app-1" || secret != "secret-9" {
		t.Fatalf("valid basic header parse failed: id=%q secret=%q ok=%v", id, secret, ok)
	}

	for _, h := range []string{
		"",                   // no header
		"Bearer xyz",         // not basic
		"Basic !!!notbase64", // bad base64
		"Basic " + base64.StdEncoding.EncodeToString([]byte("noColon")), // no colon
		"Basic " + base64.StdEncoding.EncodeToString([]byte(":onlysecret")),
		"Basic " + base64.StdEncoding.EncodeToString([]byte("onlyid:")),
	} {
		if _, _, ok := basicCredentials(mk(h)); ok {
			t.Fatalf("expected reject for header %q", h)
		}
	}
}

// guard against an accidentally huge default that would mask refresh issues.
func TestConfigDefaults(t *testing.T) {
	c := Config{}
	if c.callTimeout() != 5*time.Second {
		t.Fatalf("default call timeout = %v", c.callTimeout())
	}
	if c.verifyCacheTTL() != 60*time.Second {
		t.Fatalf("default verify cache TTL = %v", c.verifyCacheTTL())
	}
	if c.scope() != "grpc:controlplane" {
		t.Fatalf("default scope = %q", c.scope())
	}
}
