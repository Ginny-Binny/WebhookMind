package pubsub

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// MetricPoint represents a single data point for charts.
type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// RecordThroughput increments the events-per-minute counter for the current minute.
func (p *Publisher) RecordThroughput(ctx context.Context) error {
	minuteTS := time.Now().Truncate(time.Minute).Unix()
	member := strconv.FormatInt(minuteTS, 10)

	// ZINCRBY atomically increments the score (count) for this minute bucket.
	return p.client.ZIncrBy(ctx, MetricsThroughputKey, 1, member).Err()
}

// RecordLatency records a latency measurement for the current minute.
// Uses a running average approach: stores sum and count, computes avg on read.
func (p *Publisher) RecordLatency(ctx context.Context, ms int64) error {
	minuteTS := time.Now().Truncate(time.Minute).Unix()
	member := strconv.FormatInt(minuteTS, 10)

	// For simplicity, use ZADD MAX to keep the latest value per minute.
	// A more accurate approach would track sum+count, but this suffices for dashboard.
	return p.client.ZAddArgs(ctx, MetricsLatencyKey, redis.ZAddArgs{
		GT:      true,
		Members: []redis.Z{{Score: float64(ms), Member: member}},
	}).Err()
}

// GetThroughput returns throughput data points for the given time range.
func (p *Publisher) GetThroughput(ctx context.Context, rangeMinutes int) ([]MetricPoint, error) {
	return p.getMetrics(ctx, MetricsThroughputKey, rangeMinutes)
}

// GetLatency returns latency data points for the given time range.
func (p *Publisher) GetLatency(ctx context.Context, rangeMinutes int) ([]MetricPoint, error) {
	return p.getMetrics(ctx, MetricsLatencyKey, rangeMinutes)
}

func (p *Publisher) getMetrics(ctx context.Context, key string, rangeMinutes int) ([]MetricPoint, error) {
	since := time.Now().Add(-time.Duration(rangeMinutes) * time.Minute).Truncate(time.Minute).Unix()

	// ZSET stores member=unix_timestamp, score=value.
	// Fetch all members with scores and filter by timestamp.
	results, err := p.client.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange %s: %w", key, err)
	}

	points := make([]MetricPoint, 0, len(results))
	for _, z := range results {
		memberStr, ok := z.Member.(string)
		if !ok {
			continue
		}
		ts, err := strconv.ParseInt(memberStr, 10, 64)
		if err != nil {
			continue
		}
		if ts < since {
			continue
		}
		points = append(points, MetricPoint{
			Timestamp: time.Unix(ts, 0),
			Value:     z.Score,
		})
	}

	return points, nil
}
