package health

import (
	"context"
	"errors"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) HealthCheck(context.Context) error { return f.err }

func TestObjectStoreChecker(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus string
	}{
		{"reachable", nil, "healthy"},
		{"unreachable", errors.New("bucket unreachable"), "unhealthy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewObjectStoreChecker("s3", fakePinger{err: tt.err})
			got := c.Check(context.Background())
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Name != "s3" {
				t.Fatalf("name = %q, want s3", got.Name)
			}
			if tt.err != nil && got.Message != tt.err.Error() {
				t.Fatalf("message = %q, want %q", got.Message, tt.err.Error())
			}
		})
	}
}
