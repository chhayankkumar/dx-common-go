package rabbitmq

import (
	"sync"
	"testing"
)

func TestDedup_SeenAndMark(t *testing.T) {
	d := NewDedup(3)

	if d.Seen("a") {
		t.Fatal("fresh id should not be seen")
	}
	d.Mark("a")
	if !d.Seen("a") {
		t.Fatal("marked id should be seen")
	}
	// Empty ids are never seen or stored.
	if d.Seen("") {
		t.Fatal("empty id should never be seen")
	}
	d.Mark("") // no-op
}

func TestDedup_FIFOEviction(t *testing.T) {
	d := NewDedup(2)
	d.Mark("a")
	d.Mark("b")
	d.Mark("c") // evicts "a"

	if d.Seen("a") {
		t.Fatal("oldest id should have been evicted")
	}
	if !d.Seen("b") || !d.Seen("c") {
		t.Fatal("recent ids should be retained")
	}
}

func TestDedup_MarkIdempotent(t *testing.T) {
	d := NewDedup(2)
	d.Mark("a")
	d.Mark("a") // re-marking must not consume capacity
	d.Mark("b")
	if !d.Seen("a") || !d.Seen("b") {
		t.Fatal("re-marking the same id should not evict others")
	}
}

func TestDedup_Concurrent(t *testing.T) {
	d := NewDedup(128)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := string(rune('a' + n%26))
			d.Mark(id)
			_ = d.Seen(id)
		}(i)
	}
	wg.Wait()
}
