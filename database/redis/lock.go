package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrLockNotHeld is returned by Mutex.Unlock when the lock was already
// released, expired, or never successfully acquired by this Mutex instance.
var ErrLockNotHeld = errors.New("redis: lock not held")

// Mutex is a single-node distributed lock: SET NX PX with a random token,
// released via a compare-and-delete Lua script so a holder never releases a
// lock it no longer owns (e.g. one that expired and was re-acquired by
// someone else). This is deliberately NOT the multi-node Redlock algorithm
// — it is safe for the single Redis instance this platform deploys today,
// not for coordinating across independent Redis nodes.
type Mutex struct {
	c     *Client
	key   string
	ttl   time.Duration
	token string
}

// NewMutex creates a Mutex for key (namespaced under "lock:"). ttl bounds
// how long the lock is held if the holder crashes without calling Unlock —
// pick it comfortably longer than the protected critical section.
func NewMutex(c *Client, key string, ttl time.Duration) *Mutex {
	return &Mutex{c: c, key: "lock:" + key, ttl: ttl}
}

// Lock attempts to acquire the lock. Returning (false, nil) means another
// holder already has it — that is an expected outcome of losing the race,
// not an error.
func (m *Mutex) Lock(ctx context.Context) (bool, error) {
	token := uuid.NewString()
	ok, err := m.c.rdb.SetNX(ctx, m.key, token, m.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis.Mutex.Lock: %w", err)
	}
	if ok {
		m.token = token
	}
	return ok, nil
}

// unlockScript deletes the key only if it still holds this instance's
// token, so Unlock can never release a lock acquired by someone else after
// this instance's lease expired.
const unlockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`

// Unlock releases the lock, but only if this Mutex instance still holds it.
// Returns ErrLockNotHeld if Lock was never successfully called, or if the
// lock had already expired/been taken by someone else.
func (m *Mutex) Unlock(ctx context.Context) error {
	if m.token == "" {
		return ErrLockNotHeld
	}
	n, err := m.c.rdb.Eval(ctx, unlockScript, []string{m.key}, m.token).Int64()
	if err != nil {
		return fmt.Errorf("redis.Mutex.Unlock: %w", err)
	}
	m.token = ""
	if n == 0 {
		return ErrLockNotHeld
	}
	return nil
}
