package redis

import (
	"context"
	"fmt"
	"time"
)

// Allow implements a fixed-window rate limiter: at most limit calls per
// window per key, using one INCR plus a conditional EXPIRE on the first hit
// of each window — no extra bookkeeping keys. This is simpler (and
// slightly burstier at window boundaries, since a caller can spend the
// limit again right after a window rolls over) than a sliding-window or
// token-bucket limiter; use it where "roughly N per window" is good enough.
func Allow(ctx context.Context, c *Client, key string, limit int, window time.Duration) (bool, error) {
	n, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("redis.Allow: incr: %w", err)
	}
	if n == 1 {
		// First hit in this window — arm expiry. A crash between INCR and
		// EXPIRE leaves the key without a TTL; the window then never resets
		// until the key is cleared some other way. Acceptable for a rate
		// limiter (fails closed, not open) but not a durability guarantee.
		if err := c.rdb.Expire(ctx, key, window).Err(); err != nil {
			return false, fmt.Errorf("redis.Allow: expire: %w", err)
		}
	}
	return n <= int64(limit), nil
}
