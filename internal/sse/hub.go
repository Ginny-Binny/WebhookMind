package sse

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// Message represents a typed SSE event to broadcast.
type Message struct {
	Type string `json:"type"`
	Data string `json:"data"` // raw JSON string
}

// Hub manages connected SSE clients and broadcasts messages.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan Message]string // channel -> optional source_id filter ("" = all)
	logger  *slog.Logger
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[chan Message]string),
		logger:  logger,
	}
}

// Register adds a new client channel. Returns the channel to read from.
func (h *Hub) Register(sourceFilter string) chan Message {
	ch := make(chan Message, 64)
	h.mu.Lock()
	h.clients[ch] = sourceFilter
	h.mu.Unlock()
	h.logger.Debug("SSE client connected", "total_clients", h.ClientCount(), "filter", sourceFilter)
	return ch
}

// Unregister removes a client channel.
func (h *Hub) Unregister(ch chan Message) {
	h.mu.Lock()
	delete(h.clients, ch)
	close(ch)
	h.mu.Unlock()
	h.logger.Debug("SSE client disconnected", "total_clients", h.ClientCount())
}

// Broadcast sends a message to all connected clients.
// rawPayload is the full JSON envelope from Redis pub/sub.
func (h *Hub) Broadcast(rawPayload string) {
	// Parse envelope to extract type and optional source_id for filtering.
	var envelope struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawPayload), &envelope); err != nil {
		h.logger.Error("failed to parse SSE envelope", "error", err)
		return
	}

	// Extract source_id from data for filtering.
	var dataMap map[string]any
	json.Unmarshal(envelope.Data, &dataMap)
	sourceID, _ := dataMap["source_id"].(string)

	msg := Message{
		Type: envelope.Type,
		Data: string(envelope.Data),
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch, filter := range h.clients {
		if filter != "" && sourceID != "" && filter != sourceID {
			continue // client is filtering by source, and this event doesn't match
		}
		select {
		case ch <- msg:
		default:
			// Client channel is full, skip to avoid blocking.
			h.logger.Debug("SSE client buffer full, dropping message")
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
