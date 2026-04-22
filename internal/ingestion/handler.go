package ingestion

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	queue        *queue.RedisQueue
	pub          *pubsub.Publisher
	logger       *slog.Logger
	maxBodyBytes int64
}

func NewHandler(q *queue.RedisQueue, pub *pubsub.Publisher, logger *slog.Logger, maxBodyBytes int64) *Handler {
	return &Handler{
		queue:        q,
		pub:          pub,
		logger:       logger,
		maxBodyBytes: maxBodyBytes,
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

	// Capture all headers.
	headers := make(map[string]string, len(r.Header))
	for key, values := range r.Header {
		headers[key] = values[0]
	}

	eventID := uuid.New().String()

	event := &models.WebhookEvent{
		ID:         eventID,
		SourceID:   sourceID,
		ReceivedAt: time.Now().UTC(),
		RawBody:    body,
		Headers:    headers,
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
