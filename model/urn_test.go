package model

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestGenerateURN(t *testing.T) {
	got := GenerateURN("dx", "resource", "abc-123")
	if got != "urn:dx:resource:abc-123" {
		t.Fatalf("GenerateURN = %q, want urn:dx:resource:abc-123", got)
	}
}

func TestNewUUIDIsValidAndUnique(t *testing.T) {
	a := NewUUID()
	if _, err := uuid.Parse(a); err != nil {
		t.Fatalf("NewUUID produced an invalid UUID %q: %v", a, err)
	}
	if b := NewUUID(); a == b {
		t.Fatal("NewUUID returned the same value twice")
	}
	if strings.TrimSpace(a) == "" {
		t.Fatal("NewUUID returned empty")
	}
}
