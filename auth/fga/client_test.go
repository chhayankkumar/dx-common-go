package fga

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datakaveri/dx-common-go/transport/headers"
)

func TestNewValidation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("missing BaseURL must fail")
	}
	if _, err := New(Config{BaseURL: "http://x", SharedSecret: "s"}); err == nil {
		t.Fatal("SharedSecret without ServiceName must fail")
	}
	if _, err := New(Config{BaseURL: "http://x", SharedSecret: "s", ServiceName: "gateway"}); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestServiceIdentitySigned(t *testing.T) {
	secret := "platform-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := headers.Verify(r.Header, headers.Config{Secret: []byte(secret)})
		if err != nil {
			t.Errorf("verify service identity: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if user.ID != "svc:gateway" {
			t.Errorf("expected svc:gateway, got %q", user.ID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":true}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, SharedSecret: secret, ServiceName: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Check(context.Background(), CheckRequest{
		SubjectType: SubjectTypeUser, SubjectID: "u1",
		ResourceType: "databank", ResourceID: "r1", Relation: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Allowed {
		t.Fatal("expected allowed=true round trip")
	}
}
