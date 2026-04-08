package tide

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache provides distributed caching via Redis.
type Cache struct {
	client *redis.ClusterClient
}

// NewCache creates a new distributed cache.
func NewCache(client *redis.ClusterClient) *Cache {
	return &Cache{client: client}
}

// Get retrieves a value from the cache.
func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

// Set stores a value in the cache with TTL.
func (c *Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	return c.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a value from the cache.
func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
