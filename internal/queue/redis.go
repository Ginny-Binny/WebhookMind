package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/redis/go-redis/v9"
)

const (
	QueueIncoming   = "webhookmind:queue:incoming"
	QueueDelivery   = "webhookmind:queue:delivery"
	QueueExtraction = "webhookmind:queue:extraction"
	QueueDLQ        = "webhookmind:dlq"
)

type RedisQueue struct {
	client *redis.Client
}

func NewRedisQueue(addr, password string, db int) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &RedisQueue{client: client}, nil
}

func (q *RedisQueue) Enqueue(ctx context.Context, key string, event *models.WebhookEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	if err := q.client.RPush(ctx, key, data).Err(); err != nil {
		return fmt.Errorf("rpush to %s: %w", key, err)
	}

	return nil
}

func (q *RedisQueue) Dequeue(ctx context.Context, key string, timeout time.Duration) (*models.WebhookEvent, error) {
	result, err := q.client.BLPop(ctx, timeout, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		// Context cancellation is not an error during shutdown.
		if ctx.Err() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("blpop from %s: %w", key, err)
	}

	// BLPop returns [key, value].
	if len(result) < 2 {
		return nil, nil
	}

	var event models.WebhookEvent
	if err := json.Unmarshal([]byte(result[1]), &event); err != nil {
		return nil, fmt.Errorf("unmarshal event from %s: %w", key, err)
	}

	return &event, nil
}

func (q *RedisQueue) QueueLen(ctx context.Context, key string) (int64, error) {
	length, err := q.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("llen %s: %w", key, err)
	}
	return length, nil
}

// FindDLQEntry scans the Redis DLQ list and returns (raw JSON value, parsed event)
// for the first entry whose event ID matches. Both return values are empty if not found.
// The raw value is needed for LREM, which matches by value not index.
func (q *RedisQueue) FindDLQEntry(ctx context.Context, eventID string) (string, *models.WebhookEvent, error) {
	entries, err := q.client.LRange(ctx, QueueDLQ, 0, -1).Result()
	if err != nil {
		return "", nil, fmt.Errorf("lrange %s: %w", QueueDLQ, err)
	}
	for _, raw := range entries {
		var event models.WebhookEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			continue
		}
		if event.ID == eventID {
			return raw, &event, nil
		}
	}
	return "", nil, nil
}

// RemoveDLQEntry removes one occurrence of rawValue from the Redis DLQ list.
func (q *RedisQueue) RemoveDLQEntry(ctx context.Context, rawValue string) error {
	if err := q.client.LRem(ctx, QueueDLQ, 1, rawValue).Err(); err != nil {
		return fmt.Errorf("lrem %s: %w", QueueDLQ, err)
	}
	return nil
}

func (q *RedisQueue) Close() error {
	return q.client.Close()
}
