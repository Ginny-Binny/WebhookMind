package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestQueue spins up an in-process miniredis and returns a RedisQueue pointed at it,
// plus a cleanup func that closes both.
func newTestQueue(t *testing.T) (*RedisQueue, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	q, err := NewRedisQueue(mr.Addr(), "", 0)
	require.NoError(t, err)
	return q, func() {
		_ = q.Close()
		mr.Close()
	}
}

func sampleEvent(id string) *models.WebhookEvent {
	return &models.WebhookEvent{
		ID:         id,
		SourceID:   "test-source",
		ReceivedAt: time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC),
		RawBody:    []byte(`{"order_id":"ORD-1"}`),
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

func TestEnqueueDequeueRoundTrip(t *testing.T) {
	q, cleanup := newTestQueue(t)
	defer cleanup()

	ctx := context.Background()
	want := sampleEvent("evt-1")

	require.NoError(t, q.Enqueue(ctx, QueueIncoming, want))

	got, err := q.Dequeue(ctx, QueueIncoming, 1*time.Second)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.SourceID, got.SourceID)
	assert.Equal(t, want.RawBody, got.RawBody)
	assert.Equal(t, want.Headers["Content-Type"], got.Headers["Content-Type"])
}

func TestDequeueEmptyReturnsNil(t *testing.T) {
	q, cleanup := newTestQueue(t)
	defer cleanup()

	got, err := q.Dequeue(context.Background(), QueueIncoming, 100*time.Millisecond)
	require.NoError(t, err)
	assert.Nil(t, got, "dequeue on empty queue should return nil event without error")
}

func TestQueueLen(t *testing.T) {
	q, cleanup := newTestQueue(t)
	defer cleanup()

	ctx := context.Background()

	got, err := q.QueueLen(ctx, QueueDelivery)
	require.NoError(t, err)
	assert.Zero(t, got)

	for i := 0; i < 3; i++ {
		require.NoError(t, q.Enqueue(ctx, QueueDelivery, sampleEvent("evt")))
	}

	got, err = q.QueueLen(ctx, QueueDelivery)
	require.NoError(t, err)
	assert.EqualValues(t, 3, got)
}

func TestFindDLQEntry(t *testing.T) {
	q, cleanup := newTestQueue(t)
	defer cleanup()

	ctx := context.Background()

	// Seed the DLQ with three entries.
	for _, id := range []string{"evt-A", "evt-B", "evt-C"} {
		require.NoError(t, q.Enqueue(ctx, QueueDLQ, sampleEvent(id)))
	}

	t.Run("finds existing entry", func(t *testing.T) {
		raw, ev, err := q.FindDLQEntry(ctx, "evt-B")
		require.NoError(t, err)
		require.NotNil(t, ev)
		assert.Equal(t, "evt-B", ev.ID)

		// The raw value should be valid JSON containing the event ID.
		require.NotEmpty(t, raw)
		var probe models.WebhookEvent
		require.NoError(t, json.Unmarshal([]byte(raw), &probe))
		assert.Equal(t, "evt-B", probe.ID)
	})

	t.Run("returns empty result for unknown event", func(t *testing.T) {
		raw, ev, err := q.FindDLQEntry(ctx, "evt-DOES-NOT-EXIST")
		require.NoError(t, err)
		assert.Empty(t, raw)
		assert.Nil(t, ev)
	})
}

func TestRemoveDLQEntry(t *testing.T) {
	q, cleanup := newTestQueue(t)
	defer cleanup()

	ctx := context.Background()
	for _, id := range []string{"evt-A", "evt-B", "evt-C"} {
		require.NoError(t, q.Enqueue(ctx, QueueDLQ, sampleEvent(id)))
	}

	// Find then remove evt-B.
	raw, ev, err := q.FindDLQEntry(ctx, "evt-B")
	require.NoError(t, err)
	require.NotNil(t, ev)

	require.NoError(t, q.RemoveDLQEntry(ctx, raw))

	// DLQ length drops by exactly one and evt-B is no longer findable.
	got, err := q.QueueLen(ctx, QueueDLQ)
	require.NoError(t, err)
	assert.EqualValues(t, 2, got)

	_, ev, err = q.FindDLQEntry(ctx, "evt-B")
	require.NoError(t, err)
	assert.Nil(t, ev, "evt-B should be gone after RemoveDLQEntry")

	// Other entries are still present.
	_, evA, err := q.FindDLQEntry(ctx, "evt-A")
	require.NoError(t, err)
	require.NotNil(t, evA)
	_, evC, err := q.FindDLQEntry(ctx, "evt-C")
	require.NoError(t, err)
	require.NotNil(t, evC)
}
