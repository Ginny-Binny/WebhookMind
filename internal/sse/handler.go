package sse

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Handler serves SSE connections.
type Handler struct {
	hub    *Hub
	logger *slog.Logger
}

func NewHandler(hub *Hub, logger *slog.Logger) *Handler {
	return &Handler{hub: hub, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable Nginx buffering
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sourceFilter := r.URL.Query().Get("source_id")
	ch := h.hub.Register(sourceFilter)
	defer h.hub.Unregister(ch)

	// Send initial connection event.
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"ok\"}\n\n")
	flusher.Flush()

	// Keepalive ticker.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Type, msg.Data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
