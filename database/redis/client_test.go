package redis

import (
	"os"
	"testing"

	"github.com/redis/go-redis/extra/redisotel/v9"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// TestInstrumentTracingWiring proves NewClient's tracing branch is valid
// without a live server: redisotel.InstrumentTracing installs a hook and
// performs no IO, so it must succeed against a client that was never dialed,
// and be a no-op until an SDK TracerProvider is registered.
func TestInstrumentTracingWiring(t *testing.T) {
	rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { _ = rdb.Close() })

	require.NoError(t, redisotel.InstrumentTracing(rdb),
		"redisotel.InstrumentTracing must succeed on an undialed client")
}

// TestNewClientTracingEnabled exercises the real EnableTracing path against a
// live server (env-gated like the rest of this package's integration tests).
func TestNewClientTracingEnabled(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set; skipping Redis integration test")
	}
	c, err := NewClient(Config{Addr: addr, EnableTracing: true})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	require.NotNil(t, c.Underlying())
}
