package pubsub

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Subscriber listens to the Redis pub/sub channel and dispatches messages.
type Subscriber struct {
	client *redis.Client
}

func NewSubscriber(addr, password string, db int) (*Subscriber, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("subscriber redis ping: %w", err)
	}

	return &Subscriber{client: client}, nil
}

// Subscribe blocks and calls handler for each message on the channel.
// It returns when ctx is cancelled.
func (s *Subscriber) Subscribe(ctx context.Context, handler func(msg string)) error {
	sub := s.client.Subscribe(ctx, Channel)
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			handler(msg.Payload)
		}
	}
}

func (s *Subscriber) Close() error {
	return s.client.Close()
}
