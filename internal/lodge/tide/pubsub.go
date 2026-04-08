package tide

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// PubSub provides cross-instance event broadcasting via Redis pub/sub.
type PubSub struct {
	client *redis.ClusterClient
}

// NewPubSub creates a new Redis pub/sub manager.
func NewPubSub(client *redis.ClusterClient) *PubSub {
	return &PubSub{client: client}
}

// Publish sends an event to all instances subscribed to the channel.
func (ps *PubSub) Publish(ctx context.Context, channel string, payload []byte) error {
	return ps.client.Publish(ctx, channel, payload).Err()
}

// Subscribe listens for events on the given channel.
func (ps *PubSub) Subscribe(ctx context.Context, channel string) <-chan *redis.Message {
	sub := ps.client.Subscribe(ctx, channel)
	ch := sub.Channel()

	go func() {
		<-ctx.Done()
		if err := sub.Close(); err != nil {
			slog.Error("closing subscription", "error", err)
		}
	}()

	return ch
}
