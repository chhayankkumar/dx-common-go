package redis

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// testClient connects to a real Redis instance for integration tests. Set
// REDIS_TEST_ADDR (e.g. "localhost:16379") to run these; otherwise they
// skip, since a database dependency shouldn't block a plain `go test ./...`.
func testClient(t *testing.T) *Client {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set; skipping Redis integration test")
	}
	c, err := NewClient(Config{Addr: addr})
	if err != nil {
		t.Fatalf("connect to test redis at %s: %v", addr, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestMutex_LockUnlock(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	key := "test-mutex-lock-unlock"

	m1 := NewMutex(c, key, 5*time.Second)
	ok, err := m1.Lock(ctx)
	if err != nil || !ok {
		t.Fatalf("first Lock should succeed: ok=%v err=%v", ok, err)
	}

	m2 := NewMutex(c, key, 5*time.Second)
	ok, err = m2.Lock(ctx)
	if err != nil {
		t.Fatalf("second Lock errored: %v", err)
	}
	if ok {
		t.Fatal("second Lock should fail while the first holder still holds it")
	}

	if err := m1.Unlock(ctx); err != nil {
		t.Fatalf("Unlock by the holder should succeed: %v", err)
	}

	ok, err = m2.Lock(ctx)
	if err != nil || !ok {
		t.Fatalf("Lock after Unlock should succeed: ok=%v err=%v", ok, err)
	}
	_ = m2.Unlock(ctx)
}

func TestMutex_UnlockByNonHolderFails(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	key := "test-mutex-unlock-non-holder"

	m1 := NewMutex(c, key, 5*time.Second)
	if ok, err := m1.Lock(ctx); err != nil || !ok {
		t.Fatalf("Lock failed: ok=%v err=%v", ok, err)
	}
	defer m1.Unlock(ctx)

	// m2 never acquired the lock; Unlock must not release m1's lock.
	m2 := NewMutex(c, key, 5*time.Second)
	err := m2.Unlock(ctx)
	if !errors.Is(err, ErrLockNotHeld) {
		t.Fatalf("expected ErrLockNotHeld, got %v", err)
	}
}

func TestAllow_FixedWindowLimit(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	key := "test-ratelimit-" + time.Now().Format(time.RFC3339Nano)

	for i := 0; i < 3; i++ {
		ok, err := Allow(ctx, c, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("Allow call %d errored: %v", i, err)
		}
		if !ok {
			t.Fatalf("call %d should be allowed within the limit of 3", i)
		}
	}

	ok, err := Allow(ctx, c, key, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow errored: %v", err)
	}
	if ok {
		t.Fatal("4th call should be denied once the limit of 3 is exceeded")
	}
}

func TestGetOrSet_MissThenHit(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	key := "test-getorset-" + time.Now().Format(time.RFC3339Nano)

	calls := 0
	loader := func() (string, error) {
		calls++
		return "loaded-value", nil
	}

	v, err := GetOrSet(ctx, c, key, time.Minute, loader)
	if err != nil || v != "loaded-value" {
		t.Fatalf("first GetOrSet: v=%q err=%v", v, err)
	}
	if calls != 1 {
		t.Fatalf("loader should run once on a miss, ran %d times", calls)
	}

	v, err = GetOrSet(ctx, c, key, time.Minute, loader)
	if err != nil || v != "loaded-value" {
		t.Fatalf("second GetOrSet: v=%q err=%v", v, err)
	}
	if calls != 1 {
		t.Fatalf("loader should not run again on a hit, ran %d times total", calls)
	}
}
