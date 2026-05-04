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
	"github.com/gauravfs-14/webhookmind/internal/ratelimit"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSecretStore is an in-memory SecretStore for tests. Map keyed by source_id.
type stubSecretStore struct {
	secrets map[string]string
	err     error
}

func (s *stubSecretStore) GetSourceSigningSecret(_ context.Context, sourceID string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.secrets[sourceID], nil
}

// newTestHandler boots an in-process miniredis + RedisQueue + Handler with pubsub disabled.
// secrets may be nil (no source has a secret) and requireSignature defaults to false —
// preserving the original behavior of tests that predate signature verification.
func newTestHandler(t *testing.T, maxBodyBytes int64) (*Handler, *queue.RedisQueue, *miniredis.Miniredis, func()) {
	t.Helper()
	return newTestHandlerWith(t, maxBodyBytes, nil, false)
}

func newTestHandlerWith(t *testing.T, maxBodyBytes int64, secrets SecretStore, requireSignature bool) (*Handler, *queue.RedisQueue, *miniredis.Miniredis, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	q, err := queue.NewRedisQueue(mr.Addr(), "", 0)
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(q, nil /* pub */, secrets, nil /* limiter */, logger, maxBodyBytes, requireSignature)
	return h, q, mr, func() {
		_ = q.Close()
		mr.Close()
	}
}

// newTestHandlerWithLimiter is for tests that exercise the rate-limit path.
func newTestHandlerWithLimiter(t *testing.T, perIP, perSource int) (*Handler, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	q, err := queue.NewRedisQueue(mr.Addr(), "", 0)
	require.NoError(t, err)
	rlClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	limiter := ratelimit.NewLimiter(rlClient, perIP, perSource)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(q, nil, nil, limiter, logger, 1<<20, false)
	return h, func() {
		_ = q.Close()
		_ = rlClient.Close()
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

// --- HMAC signature behavior matrix ---

// Case: source has no secret AND require_signature=false → accept (back-compat for dev/test).
func TestSignature_UnsignedSource_AllowedByDefault(t *testing.T) {
	h, _, _, cleanup := newTestHandlerWith(t, 1<<20, &stubSecretStore{secrets: map[string]string{}}, false)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{"k":"v"}`))
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// Case: source has no secret AND require_signature=true → 401 (production gate).
func TestSignature_UnsignedSource_RejectedWhenRequired(t *testing.T) {
	h, q, _, cleanup := newTestHandlerWith(t, 1<<20, &stubSecretStore{secrets: map[string]string{}}, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{"k":"v"}`))
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	// Nothing should be enqueued.
	depth, err := q.QueueLen(context.Background(), queue.QueueIncoming)
	require.NoError(t, err)
	assert.Zero(t, depth)
}

// Case: source has a secret AND request is properly signed → accept.
func TestSignature_SignedSource_ValidSignature_Accepted(t *testing.T) {
	const secret = "test-source-secret"
	h, q, _, cleanup := newTestHandlerWith(t, 1<<20,
		&stubSecretStore{secrets: map[string]string{"test-source": secret}}, false)
	defer cleanup()

	body := []byte(`{"order_id":"SIGNED-1"}`)
	header := SignForTest(secret, body, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(string(body)))
	req.Header.Set("X-Signature", header)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	got, err := q.Dequeue(context.Background(), queue.QueueIncoming, 500*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.JSONEq(t, string(body), string(got.RawBody))
}

// Case: source has a secret AND header is missing → 401.
func TestSignature_SignedSource_MissingHeader_Rejected(t *testing.T) {
	h, q, _, cleanup := newTestHandlerWith(t, 1<<20,
		&stubSecretStore{secrets: map[string]string{"test-source": "secret"}}, false)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	depth, err := q.QueueLen(context.Background(), queue.QueueIncoming)
	require.NoError(t, err)
	assert.Zero(t, depth)
}

// Case: source has a secret AND signature is wrong → 401.
func TestSignature_SignedSource_WrongSignature_Rejected(t *testing.T) {
	const secret = "the-real-secret"
	h, q, _, cleanup := newTestHandlerWith(t, 1<<20,
		&stubSecretStore{secrets: map[string]string{"test-source": secret}}, false)
	defer cleanup()

	body := []byte(`{"x":1}`)
	// Sign with the wrong key.
	header := SignForTest("a-different-secret", body, time.Now())

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(string(body)))
	req.Header.Set("X-Signature", header)
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	depth, err := q.QueueLen(context.Background(), queue.QueueIncoming)
	require.NoError(t, err)
	assert.Zero(t, depth)
}

// Case: secret-store lookup itself fails (e.g., DB transient error) → 500, not enqueued.
func TestSignature_SecretStoreError_Returns500(t *testing.T) {
	h, q, _, cleanup := newTestHandlerWith(t, 1<<20,
		&stubSecretStore{err: errBoom}, false)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	depth, err := q.QueueLen(context.Background(), queue.QueueIncoming)
	require.NoError(t, err)
	assert.Zero(t, depth)
}

var errBoom = stubError("simulated db error")

type stubError string

func (e stubError) Error() string { return string(e) }

// --- end HMAC tests ---

func TestHandleWebhook_RateLimited(t *testing.T) {
	// Allow exactly 2 requests/min per IP. The third should get 429.
	h, cleanup := newTestHandlerWithLimiter(t, 2, 0)
	defer cleanup()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
		req.RemoteAddr = "203.0.113.1:5000"
		rec := httptest.NewRecorder()
		h.Router().ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code, "request %d should be accepted", i+1)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req.RemoteAddr = "203.0.113.1:5000"
	rec := httptest.NewRecorder()
	h.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"), "Retry-After header must be set on 429")
	assert.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	assert.Contains(t, rec.Body.String(), `"scope":"ip"`)
}

func TestHandleWebhook_RateLimitScopedByIP(t *testing.T) {
	h, cleanup := newTestHandlerWithLimiter(t, 1, 0)
	defer cleanup()

	// Drain ip-A's quota.
	req1 := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	h.Router().ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusAccepted, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req2.RemoteAddr = "10.0.0.1:1234"
	rec2 := httptest.NewRecorder()
	h.Router().ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)

	// ip-B has its own bucket.
	req3 := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req3.RemoteAddr = "10.0.0.2:1234"
	rec3 := httptest.NewRecorder()
	h.Router().ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusAccepted, rec3.Code)
}

func TestHandleWebhook_HonorsXForwardedFor(t *testing.T) {
	// 1 req/min per IP. Two different XFF values should each get one through.
	h, cleanup := newTestHandlerWithLimiter(t, 1, 0)
	defer cleanup()

	// Both requests come from the same RemoteAddr (the Caddy/Traefik proxy), but the
	// X-Forwarded-For header carries the real client IP — and the limiter should key off
	// XFF so legitimate users behind the same proxy don't share a quota.
	req1 := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req1.RemoteAddr = "172.18.0.5:80"
	req1.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec1 := httptest.NewRecorder()
	h.Router().ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusAccepted, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/webhook/test-source", strings.NewReader(`{}`))
	req2.RemoteAddr = "172.18.0.5:80"
	req2.Header.Set("X-Forwarded-For", "198.51.100.20")
	rec2 := httptest.NewRecorder()
	h.Router().ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusAccepted, rec2.Code, "second request from a different XFF should NOT be rate-limited")
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
