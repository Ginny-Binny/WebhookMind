package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Envelope is the JSON structure published to the Redis channel.
type Envelope struct {
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"ts"`
}

// Publisher publishes typed events to Redis pub/sub for SSE fan-out.
type Publisher struct {
	client *redis.Client
}

func NewPublisher(addr, password string, db int) (*Publisher, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("pubsub redis ping: %w", err)
	}

	return &Publisher{client: client}, nil
}

// Publish sends a typed event to the Redis pub/sub channel.
func (p *Publisher) Publish(ctx context.Context, eventType string, data any) error {
	env := Envelope{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal pubsub envelope: %w", err)
	}

	return p.client.Publish(ctx, Channel, payload).Err()
}

// Client returns the underlying Redis client for metrics operations.
func (p *Publisher) Client() *redis.Client {
	return p.client
}

func (p *Publisher) Close() error {
	return p.client.Close()
}
