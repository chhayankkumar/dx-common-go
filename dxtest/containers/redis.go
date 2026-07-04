package containers

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	redistc "github.com/testcontainers/testcontainers-go/modules/redis"

	dxredis "github.com/datakaveri/dx-common-go/database/redis"
)

// RedisHandle is a ready-to-use Redis connection for a test.
type RedisHandle struct {
	Client *dxredis.Client
	Addr   string
}

var (
	redisOnce sync.Once
	redisAddr string
	redisErr  error
)

// Redis returns a RedisHandle bound to a real Redis instance.
//
// If DX_TEST_REDIS_ADDR is set, it binds to that external instance.
// Otherwise it starts one testcontainers-go Redis container, shared across
// every Redis(t) call in this test binary — same "one container per binary,
// left for the reaper to clean up" rationale as Postgres.
func Redis(t *testing.T) *RedisHandle {
	t.Helper()
	addr := os.Getenv("DX_TEST_REDIS_ADDR")
	if addr == "" {
		addr = startRedisContainer(t)
	}

	client, err := dxredis.NewClient(dxredis.Config{Addr: addr})
	if err != nil {
		t.Fatalf("dxtest/containers: connect redis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return &RedisHandle{Client: client, Addr: addr}
}

func startRedisContainer(t *testing.T) string {
	t.Helper()
	// See startPostgresContainer's comment: every caller checks Docker
	// health for itself, even though the container start below only
	// happens once.
	testcontainers.SkipIfProviderIsNotHealthy(t)

	redisOnce.Do(func() {
		ctx := context.Background()
		container, err := redistc.Run(ctx, "redis:7-alpine")
		if err != nil {
			redisErr = fmt.Errorf("start redis container: %w", err)
			return
		}
		connStr, err := container.ConnectionString(ctx)
		if err != nil {
			redisErr = fmt.Errorf("redis container connection string: %w", err)
			return
		}
		redisAddr = trimRedisScheme(connStr)
	})
	if redisErr != nil {
		t.Fatalf("dxtest/containers: %v", redisErr)
	}
	return redisAddr
}

// trimRedisScheme strips a leading "redis://" from connStr — the container's
// ConnectionString returns a full URL, but dxredis.Config.Addr wants a bare
// host:port. Returns connStr unchanged if it has no such prefix.
func trimRedisScheme(connStr string) string {
	const scheme = "redis://"
	if len(connStr) > len(scheme) && connStr[:len(scheme)] == scheme {
		return connStr[len(scheme):]
	}
	return connStr
}
