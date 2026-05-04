package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const signatureMaxAge = 5 * time.Minute

// SecretStore is the minimal interface the handler needs to look up per-source signing secrets.
// Satisfied by *store.PostgresStore. Defined here so tests can inject a stub without touching the DB.
type SecretStore interface {
	GetSourceSigningSecret(ctx context.Context, sourceID string) (string, error)
}

type Handler struct {
	queue            *queue.RedisQueue
	pub              *pubsub.Publisher
	secrets          SecretStore
	logger           *slog.Logger
	maxBodyBytes     int64
	requireSignature bool
}

func NewHandler(
	q *queue.RedisQueue,
	pub *pubsub.Publisher,
	secrets SecretStore,
	logger *slog.Logger,
	maxBodyBytes int64,
	requireSignature bool,
) *Handler {
	return &Handler{
		queue:            q,
		pub:              pub,
		secrets:          secrets,
		logger:           logger,
		maxBodyBytes:     maxBodyBytes,
		requireSignature: requireSignature,
	}
}

func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/webhook/{source_id}", h.handleWebhook)
	r.Get("/health", h.handleHealth)
	return r
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "source_id")
	if sourceID == "" {
		http.Error(w, `{"error":"source_id is required"}`, http.StatusBadRequest)
		return
	}

	// Limit body size.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body",
			"component", "ingestion",
			"source_id", sourceID,
			"error", err,
		)
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	// Verify HMAC signature (if the source has a secret configured, or if signing is globally required).
	if !h.checkSignature(w, r, sourceID, body) {
		// checkSignature has already logged + written the response.
		return
	}

	// BYOK: extract the per-request Anthropic API key BEFORE building the headers map so it
	// never lands in the persisted event. The extractor-bridge picks it up via the transient
	// APIKeyOverride field on the in-flight queue payload.
	apiKeyOverride := r.Header.Get("X-Anthropic-Key")

	// Capture all headers, redacting the BYOK key so it doesn't leak into the dashboard
	// payload view, the Scylla webhook_events row, or any logs.
	headers := make(map[string]string, len(r.Header))
	for key, values := range r.Header {
		if strings.EqualFold(key, "X-Anthropic-Key") {
			continue
		}
		headers[key] = values[0]
	}

	eventID := uuid.New().String()

	event := &models.WebhookEvent{
		ID:             eventID,
		SourceID:       sourceID,
		ReceivedAt:     time.Now().UTC(),
		RawBody:        body,
		Headers:        headers,
		APIKeyOverride: apiKeyOverride,
	}

	if err := h.queue.Enqueue(r.Context(), queue.QueueIncoming, event); err != nil {
		h.logger.Error("failed to enqueue event",
			"component", "ingestion",
			"event_id", eventID,
			"source_id", sourceID,
			"error", err,
		)
		http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	h.logger.Debug("event enqueued",
		"component", "ingestion",
		"event_id", eventID,
		"source_id", sourceID,
	)

	if h.pub != nil {
		h.pub.Publish(r.Context(), pubsub.EventWebhookReceived, map[string]any{
			"event_id":    eventID,
			"source_id":   sourceID,
			"received_at": event.ReceivedAt,
		})
		h.pub.RecordThroughput(r.Context())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]string{"id": eventID}); err != nil {
		h.logger.Error("failed to write response",
			"component", "ingestion",
			"event_id", eventID,
			"source_id", sourceID,
			"error", err,
		)
	}
}

// checkSignature returns true if the request is allowed through, false if it was rejected
// (in which case it has already written the 401 response and logged the reason).
//
// Behavior matrix:
//   - secret empty + requireSignature=false  → accept (back-compat for unsigned sources)
//   - secret empty + requireSignature=true   → 401
//   - secret set                              → 401 unless X-Signature matches
func (h *Handler) checkSignature(w http.ResponseWriter, r *http.Request, sourceID string, body []byte) bool {
	var secret string
	if h.secrets != nil {
		var err error
		secret, err = h.secrets.GetSourceSigningSecret(r.Context(), sourceID)
		if err != nil {
			h.logger.Error("failed to look up signing secret",
				"component", "ingestion",
				"source_id", sourceID,
				"error", err,
			)
			http.Error(w, `{"error":"failed to verify signature"}`, http.StatusInternalServerError)
			return false
		}
	}

	if secret == "" {
		if !h.requireSignature {
			return true // unsigned-source path, dev/test
		}
		h.logger.Warn("rejecting unsigned webhook (REQUIRE_SIGNATURE=true)",
			"component", "ingestion",
			"source_id", sourceID,
		)
		http.Error(w, `{"error":"signature required but source has no secret configured"}`, http.StatusUnauthorized)
		return false
	}

	if err := Verify(secret, r.Header.Get("X-Signature"), body, time.Now(), signatureMaxAge); err != nil {
		h.logger.Warn("signature verification failed",
			"component", "ingestion",
			"source_id", sourceID,
			"reason", classifySignatureError(err),
		)
		http.Error(w, `{"error":"signature verification failed"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

// classifySignatureError returns a short stable string for logging — avoids leaking
// full error messages while still letting operators tell rejection modes apart.
func classifySignatureError(err error) string {
	switch {
	case errors.Is(err, ErrMissingHeader):
		return "missing_header"
	case errors.Is(err, ErrMalformedHeader):
		return "malformed_header"
	case errors.Is(err, ErrStaleTimestamp):
		return "stale_timestamp"
	case errors.Is(err, ErrSignatureMismatch):
		return "signature_mismatch"
	case errors.Is(err, ErrInvalidSecret):
		return "invalid_secret"
	default:
		return "unknown"
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		h.logger.Error("failed to write health response",
			"component", "ingestion",
			"error", err,
		)
	}
}
