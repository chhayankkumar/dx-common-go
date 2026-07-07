package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/datakaveri/dx-common-go/cache"
)

// NewCache adapts a redis Client to the platform cache.Cache contract — the
// production implementation behind cache.GetOrLoad. Options set a key prefix
// (also scoping Clear) and a default TTL applied when Set receives ttl <= 0.
func NewCache(c *Client, opts ...CacheOption) cache.Cache {
	rc := &redisCache{c: c}
	for _, o := range opts {
		o(rc)
	}
	return rc
}

// CacheOption configures NewCache.
type CacheOption func(*redisCache)

// WithKeyPrefix namespaces every key (and makes Clear safe: it deletes only
// the prefix, never the whole DB).
func WithKeyPrefix(p string) CacheOption { return func(r *redisCache) { r.prefix = p } }

// WithDefaultTTL applies when Set is called with ttl <= 0.
func WithDefaultTTL(d time.Duration) CacheOption { return func(r *redisCache) { r.ttl = d } }

type redisCache struct {
	c      *Client
	prefix string
	ttl    time.Duration
}

func (r *redisCache) key(k string) string { return r.prefix + k }

func (r *redisCache) Get(ctx context.Context, key string) (string, error) {
	s, err := r.c.rdb.Get(ctx, r.key(key)).Result()
	if err != nil {
		return "", err // cache.GetOrLoad treats any error as a miss
	}
	return s, nil
}

func (r *redisCache) GetJSON(ctx context.Context, key string, dest interface{}) error {
	s, err := r.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(s), dest)
}

func (r *redisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = r.ttl
	}
	var payload string
	switch v := value.(type) {
	case string:
		payload = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("redis cache set %q: marshal: %w", key, err)
		}
		payload = string(b)
	}
	return r.c.rdb.Set(ctx, r.key(key), payload, ttl).Err()
}

func (r *redisCache) Delete(ctx context.Context, key string) error {
	return r.c.rdb.Del(ctx, r.key(key)).Err()
}

func (r *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.c.rdb.Exists(ctx, r.key(key)).Result()
	return n > 0, err
}

// Clear deletes every key under the configured prefix (SCAN+DEL). Without a
// prefix it refuses rather than flushing a shared database.
func (r *redisCache) Clear(ctx context.Context) error {
	if r.prefix == "" {
		return fmt.Errorf("redis cache: Clear requires WithKeyPrefix (refusing to flush a shared DB)")
	}
	iter := r.c.rdb.Scan(ctx, 0, r.prefix+"*", 200).Iterator()
	for iter.Next(ctx) {
		if err := r.c.rdb.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}
