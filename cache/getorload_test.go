package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetOrLoad_MissLoadsAndCaches(t *testing.T) {
	c := NewMemoryCache()
	got, err := GetOrLoad(context.Background(), c, "k1", time.Minute, func(context.Context) (int, error) { return 42, nil })
	if err != nil || got != 42 {
		t.Fatalf("got %d err %v", got, err)
	}
	// second call must hit the cache, not the loader
	got2, err := GetOrLoad(context.Background(), c, "k1", time.Minute, func(context.Context) (int, error) {
		t.Fatal("loader must not run on hit")
		return 0, nil
	})
	if err != nil || got2 != 42 {
		t.Fatalf("hit path got %d err %v", got2, err)
	}
}

func TestGetOrLoad_SingleflightDedup(t *testing.T) {
	c := NewMemoryCache()
	var calls atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = GetOrLoad(context.Background(), c, "hot", time.Minute, func(context.Context) (string, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return "v", nil
			})
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("loader ran %d times, want 1", n)
	}
}

func TestGetOrLoad_LoadErrorNotCached(t *testing.T) {
	c := NewMemoryCache()
	boom := errors.New("boom")
	if _, err := GetOrLoad(context.Background(), c, "e", time.Minute, func(context.Context) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	// next call must retry the loader (error was not cached)
	got, err := GetOrLoad(context.Background(), c, "e", time.Minute, func(context.Context) (int, error) { return 7, nil })
	if err != nil || got != 7 {
		t.Fatalf("retry got %d err %v", got, err)
	}
}
