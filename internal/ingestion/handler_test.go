package ingestion

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestHandler boots an in-process miniredis + RedisQueue + Handler with pubsub disabled.
func newTestHandler(t *testing.T, maxBodyBytes int64) (*Handler, *queue.RedisQueue, *miniredis.Miniredis, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	q, err := queue.NewRedisQueue(mr.Addr(), "", 0)
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(q, nil /* pub */, logger, maxBodyBytes)
	return h, q, mr, func() {
		_ = q.Close()
		mr.Close()
	}
}

func TestHandleWebhook_AcceptsAndEnqueues(t *testing.T) {
	h, q, _, cleanup := newTestHandler(t, 1<<20) // 1 MiB
	defer cleanup()

	body := strings.NewReader(`{"order_id":"ORD-1","amount":500}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Router().ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	// Response is JSON {"id":"<uuid>"}.
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["id"], "response should include the generated event id")

	// The event landed in the incoming queue with the expected payload.
	dequeued, err := q.Dequeue(context.Background(), queue.QueueIncoming, 500*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, dequeued)
	assert.Equal(t, "test-source", dequeued.SourceID)
	assert.JSONEq(t, `{"order_id":"ORD-1","amount":500}`, string(dequeued.RawBody))
	assert.Equal(t, resp["id"], dequeued.ID, "queued event id should match the one returned to the caller")
	assert.Equal(t, "application/json", dequeued.Headers["Content-Type"])
}

func TestHandleWebhook_RejectsOversizedBody(t *testing.T) {
	const limit = int64(64) // tiny, easy to exceed
	h, q, _, cleanup := newTestHandler(t, limit)
	defer cleanup()

	// Body larger than the limit.
	big := strings.Repeat("a", int(limit)+200)
	body := strings.NewReader(`{"x":"` + big + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", body)
	rec := httptest.NewRecorder()

	h.Router().ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400, "oversized body should not return 2xx")
	assert.Less(t, rec.Code, 500, "oversized body is a client error, not server error")

	// Nothing should have been queued.
	depth, err := q.QueueLen(context.Background(), queue.QueueIncoming)
	require.NoError(t, err)
	assert.Zero(t, depth)
}

func TestHandleWebhook_PreservesHeaders(t *testing.T) {
	h, q, _, cleanup := newTestHandler(t, 1<<20)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req.Header.Set("X-Source-Token", "abc123")
	req.Header.Set("X-Custom", "yes")
	rec := httptest.NewRecorder()

	h.Router().ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	dequeued, err := q.Dequeue(context.Background(), queue.QueueIncoming, 500*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, dequeued)
	assert.Equal(t, "abc123", dequeued.Headers["X-Source-Token"])
	assert.Equal(t, "yes", dequeued.Headers["X-Custom"])
}

func TestHealthEndpoint(t *testing.T) {
	h, _, _, cleanup := newTestHandler(t, 1<<20)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}

// Sanity check that the WebhookEvent shape we expect is what the queue actually carries.
func TestEnqueuedEventShape(t *testing.T) {
	h, q, _, cleanup := newTestHandler(t, 1<<20)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{"k":"v"}`))
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	got, err := q.Dequeue(context.Background(), queue.QueueIncoming, 500*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Confirm fields the rest of the pipeline depends on.
	assert.IsType(t, "", got.ID)
	assert.IsType(t, "", got.SourceID)
	assert.IsType(t, time.Time{}, got.ReceivedAt)
	assert.IsType(t, []byte{}, got.RawBody)
	assert.IsType(t, map[string]string{}, got.Headers)

	// The event satisfies the runtime contract used downstream.
	var probe models.WebhookEvent = *got
	_ = probe
}
