package ratelimit

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLimiter spins up an in-process miniredis and returns a Limiter pointed at it.
func newTestLimiter(t *testing.T, perIP, perSource int) *Limiter {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewLimiter(client, perIP, perSource)
}

func TestAllowIP_BelowLimitAllowed(t *testing.T) {
	l := newTestLimiter(t, 5, 0)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		res, err := l.AllowIP(ctx, "1.2.3.4")
		require.NoError(t, err)
		assert.True(t, res.Allowed, "request %d should be allowed", i+1)
	}
}

func TestAllowIP_BlocksWhenExceeded(t *testing.T) {
	l := newTestLimiter(t, 3, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		res, err := l.AllowIP(ctx, "1.2.3.4")
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}

	res, err := l.AllowIP(ctx, "1.2.3.4")
	require.NoError(t, err)
	assert.False(t, res.Allowed, "request 4 should be rate-limited")
	assert.Greater(t, res.RetryAfter.Seconds(), 0.0, "RetryAfter should be set when blocked")
}

func TestAllowIP_LimitsAreIPScoped(t *testing.T) {
	l := newTestLimiter(t, 2, 0)
	ctx := context.Background()

	// Drain ip-A's quota.
	for i := 0; i < 2; i++ {
		res, err := l.AllowIP(ctx, "10.0.0.1")
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}
	res, err := l.AllowIP(ctx, "10.0.0.1")
	require.NoError(t, err)
	require.False(t, res.Allowed, "ip-A should be rate-limited")

	// ip-B has its own bucket and should be unaffected.
	res, err = l.AllowIP(ctx, "10.0.0.2")
	require.NoError(t, err)
	assert.True(t, res.Allowed, "ip-B's bucket should be independent of ip-A's")
}

func TestAllowSource_LimitsAreSourceScoped(t *testing.T) {
	l := newTestLimiter(t, 0, 2)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		res, err := l.AllowSource(ctx, "stripe-prod")
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}
	res, err := l.AllowSource(ctx, "stripe-prod")
	require.NoError(t, err)
	require.False(t, res.Allowed, "stripe-prod source should be rate-limited")

	res, err = l.AllowSource(ctx, "github-prod")
	require.NoError(t, err)
	assert.True(t, res.Allowed, "github-prod's bucket should be independent")
}

func TestAllowIP_ZeroLimitDisables(t *testing.T) {
	l := newTestLimiter(t, 0, 0)
	ctx := context.Background()

	// 100 fast requests — none should be blocked because IP limit is disabled.
	for i := 0; i < 100; i++ {
		res, err := l.AllowIP(ctx, "1.2.3.4")
		require.NoError(t, err)
		require.True(t, res.Allowed, "limit=0 must disable rate limiting")
	}
}

func TestAllowSource_ZeroLimitDisables(t *testing.T) {
	l := newTestLimiter(t, 0, 0)
	ctx := context.Background()

	for i := 0; i < 100; i++ {
		res, err := l.AllowSource(ctx, "test-source")
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}
}

func TestResult_RemainingDecreasesAsQuotaIsConsumed(t *testing.T) {
	l := newTestLimiter(t, 5, 0)
	ctx := context.Background()

	res1, err := l.AllowIP(ctx, "1.2.3.4")
	require.NoError(t, err)
	require.True(t, res1.Allowed)

	res2, err := l.AllowIP(ctx, "1.2.3.4")
	require.NoError(t, err)
	require.True(t, res2.Allowed)

	assert.Less(t, res2.Remaining, res1.Remaining, "Remaining should decrease as the bucket drains")
}
