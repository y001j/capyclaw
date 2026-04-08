package tide

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// PresenceTracker manages user online/offline status via Redis.
type PresenceTracker struct {
	client *redis.ClusterClient
	ttl    time.Duration
}

// NewPresenceTracker creates a new presence tracker.
func NewPresenceTracker(client *redis.ClusterClient, ttl time.Duration) *PresenceTracker {
	return &PresenceTracker{client: client, ttl: ttl}
}

// SetOnline marks a user as online.
func (t *PresenceTracker) SetOnline(ctx context.Context, userID string) error {
	return t.client.Set(ctx, "presence:"+userID, "online", t.ttl).Err()
}

// SetOffline removes a user's online status.
func (t *PresenceTracker) SetOffline(ctx context.Context, userID string) error {
	return t.client.Del(ctx, "presence:"+userID).Err()
}

// IsOnline checks if a user is currently online.
func (t *PresenceTracker) IsOnline(ctx context.Context, userID string) (bool, error) {
	result, err := t.client.Exists(ctx, "presence:"+userID).Result()
	return result > 0, err
}

// Heartbeat refreshes a user's online TTL.
func (t *PresenceTracker) Heartbeat(ctx context.Context, userID string) error {
	return t.client.Expire(ctx, "presence:"+userID, t.ttl).Err()
}
