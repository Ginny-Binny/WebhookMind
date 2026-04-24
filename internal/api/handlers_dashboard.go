package api

import (
	"encoding/json"
	"net/http"

	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/go-chi/chi/v5"
)

// --- Sources ---

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.pg.ListSources(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list sources"}`, http.StatusInternalServerError)
		return
	}
	if sources == nil {
		w.Write([]byte("[]"))
		return
	}
	json.NewEncoder(w).Encode(sources)
}

// --- Webhooks ---

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	items, err := s.pg.ListWebhooks(r.Context(), sourceID, limit, offset)
	if err != nil {
		http.Error(w, `{"error":"failed to list webhooks"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(items)
}

func (s *Server) handleGetWebhookDetail(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "eventID")

	detail, err := s.pg.GetWebhookDetail(r.Context(), eventID)
	if err != nil {
		http.Error(w, `{"error":"webhook not found"}`, http.StatusNotFound)
		return
	}

	// Fetch raw body from ScyllaDB if available.
	if s.scylla != nil && detail.SourceID != "" {
		event, err := s.scylla.GetEvent(detail.SourceID, eventID)
		if err == nil && event != nil {
			detail.RawBody = event.RawBody
		}
	}

	json.NewEncoder(w).Encode(detail)
}

// --- DLQ ---

func (s *Server) handleListDLQ(w http.ResponseWriter, r *http.Request) {
	sourceID := r.URL.Query().Get("source_id")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	entries, err := s.pg.ListDLQEntries(r.Context(), sourceID, limit, offset)
	if err != nil {
		http.Error(w, `{"error":"failed to list DLQ entries"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) handleRetryDLQ(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "eventID")

	if s.queue == nil {
		http.Error(w, `{"error":"queue unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	rawValue, event, err := s.queue.FindDLQEntry(r.Context(), eventID)
	if err != nil {
		http.Error(w, `{"error":"failed to read DLQ"}`, http.StatusInternalServerError)
		return
	}
	if event == nil {
		http.Error(w, `{"error":"DLQ entry not found"}`, http.StatusNotFound)
		return
	}

	if err := s.queue.Enqueue(r.Context(), queue.QueueDelivery, event); err != nil {
		http.Error(w, `{"error":"failed to re-enqueue for delivery"}`, http.StatusInternalServerError)
		return
	}

	// Best-effort cleanup: remove from Redis DLQ list and mark the Postgres row resolved.
	// Failures here are logged-but-tolerated because the event is already back in the delivery queue.
	_ = s.queue.RemoveDLQEntry(r.Context(), rawValue)
	_ = s.pg.ResolveDLQEntry(r.Context(), eventID)

	json.NewEncoder(w).Encode(map[string]string{"status": "retrying", "event_id": eventID})
}

func (s *Server) handleDiscardDLQ(w http.ResponseWriter, r *http.Request) {
	eventID := chi.URLParam(r, "eventID")

	if s.queue != nil {
		if rawValue, _, err := s.queue.FindDLQEntry(r.Context(), eventID); err == nil && rawValue != "" {
			_ = s.queue.RemoveDLQEntry(r.Context(), rawValue)
		}
	}

	if err := s.pg.ResolveDLQEntry(r.Context(), eventID); err != nil {
		http.Error(w, `{"error":"failed to mark DLQ entry resolved"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "discarded", "event_id": eventID})
}

// --- Metrics ---

func (s *Server) handleThroughputMetrics(w http.ResponseWriter, r *http.Request) {
	rangeMin := queryInt(r, "range", 60)

	if s.pub == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}

	points, err := s.pub.GetThroughput(r.Context(), rangeMin)
	if err != nil {
		http.Error(w, `{"error":"failed to get throughput metrics"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(points)
}

func (s *Server) handleLatencyMetrics(w http.ResponseWriter, r *http.Request) {
	rangeMin := queryInt(r, "range", 60)

	if s.pub == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}

	points, err := s.pub.GetLatency(r.Context(), rangeMin)
	if err != nil {
		http.Error(w, `{"error":"failed to get latency metrics"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(points)
}

