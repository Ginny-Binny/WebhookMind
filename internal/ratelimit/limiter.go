// Package ratelimit wraps redis_rate (GCRA algorithm) so the ingestion handler can
// reject abusive callers with a single Redis round-trip per check.
//
// Two scopes are supported:
//   - Per-IP: catches generic abuse from a single client.
//   - Per-source: catches a single misbehaving webhook sender flooding the system.
//
// Both keys use the existing Redis instance, namespaced under "webhookmind:ratelimit:...".
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// Result is a transport-agnostic view of a rate-limit decision.
type Result struct {
	Allowed    bool          // false = caller should be told 429
	Remaining  int           // requests left in the current window
	RetryAfter time.Duration // hint for the Retry-After header (only meaningful when !Allowed)
	ResetAfter time.Duration // when the window fully resets
}

// Limiter is a thin facade over redis_rate that knows the two limit scopes
// the ingestion handler needs.
type Limiter struct {
	rl                 *redis_rate.Limiter
	perIPPerMinute     int
	perSourcePerMinute int
}

// NewLimiter builds a Limiter against the given Redis client. Pass 0 (or negative)
// for either limit to disable that check entirely.
func NewLimiter(client *redis.Client, perIPPerMinute, perSourcePerMinute int) *Limiter {
	return &Limiter{
		rl:                 redis_rate.NewLimiter(client),
		perIPPerMinute:     perIPPerMinute,
		perSourcePerMinute: perSourcePerMinute,
	}
}

// AllowIP checks whether the given client IP can make another request right now.
func (l *Limiter) AllowIP(ctx context.Context, ip string) (*Result, error) {
	if l.perIPPerMinute <= 0 {
		return &Result{Allowed: true}, nil
	}
	return l.allow(ctx, "webhookmind:ratelimit:ip:"+ip, l.perIPPerMinute)
}

// AllowSource checks whether the given source can ingest another webhook right now.
func (l *Limiter) AllowSource(ctx context.Context, sourceID string) (*Result, error) {
	if l.perSourcePerMinute <= 0 {
		return &Result{Allowed: true}, nil
	}
	return l.allow(ctx, "webhookmind:ratelimit:source:"+sourceID, l.perSourcePerMinute)
}

func (l *Limiter) allow(ctx context.Context, key string, perMinute int) (*Result, error) {
	res, err := l.rl.Allow(ctx, key, redis_rate.PerMinute(perMinute))
	if err != nil {
		return nil, fmt.Errorf("rate limit check: %w", err)
	}
	return &Result{
		Allowed:    res.Allowed > 0,
		Remaining:  res.Remaining,
		RetryAfter: res.RetryAfter,
		ResetAfter: res.ResetAfter,
	}, nil
}
