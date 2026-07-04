package scheduler

import "testing"

// TestLockKey_Deterministic pins that the same job name always derives the
// same advisory-lock key (required for WithSingleton to actually coordinate
// across replicas — each must compute the same key for the same job) and
// that distinct names derive distinct keys.
func TestLockKey_Deterministic(t *testing.T) {
	a1 := lockKey("acl-outbox-dispatch")
	a2 := lockKey("acl-outbox-dispatch")
	if a1 != a2 {
		t.Fatalf("lockKey not deterministic: %d != %d", a1, a2)
	}

	b := lockKey("authz-outbox-dispatch")
	if a1 == b {
		t.Fatalf("lockKey collision between distinct names: both = %d", a1)
	}
}
