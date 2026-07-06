package health

import (
	"context"
	"errors"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) HealthCheck(context.Context) error { return f.err }

func TestPingCheckers(t *testing.T) {
	tests := []struct {
		name     string
		checker  *PingChecker
		wantName string
	}{
		{"object store", NewObjectStoreChecker("s3", fakePinger{}), "s3"},
		{"elasticsearch", NewElasticsearchChecker(fakePinger{}), "elasticsearch"},
		{"generic", NewPingChecker("thing", fakePinger{}), "thing"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/healthy", func(t *testing.T) {
			got := tt.checker.Check(context.Background())
			if got.Status != "healthy" || got.Name != tt.wantName {
				t.Fatalf("got (%q,%q), want (healthy,%q)", got.Status, got.Name, tt.wantName)
			}
		})
	}

	t.Run("unhealthy surfaces the error", func(t *testing.T) {
		err := errors.New("cluster unreachable")
		got := NewElasticsearchChecker(fakePinger{err: err}).Check(context.Background())
		if got.Status != "unhealthy" || got.Message != err.Error() {
			t.Fatalf("got (%q,%q), want (unhealthy,%q)", got.Status, got.Message, err.Error())
		}
	})
}
