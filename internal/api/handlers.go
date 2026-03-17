package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// --- Schema & Drift ---

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	schema, err := s.pg.GetPayloadSchema(r.Context(), sourceID)
	if err != nil {
		http.Error(w, `{"error":"schema not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(schema)
}

func (s *Server) handleGetDrifts(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	limit := queryInt(r, "limit", 50)
	events, err := s.pg.GetDriftEvents(r.Context(), sourceID, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch drifts"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(events)
}

// --- Diffs ---

func (s *Server) handleGetDiffs(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	limit := queryInt(r, "limit", 50)
	diffs, err := s.pg.GetWebhookDiffs(r.Context(), sourceID, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch diffs"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(diffs)
}

// --- Routing Rules ---

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	var input struct {
		DestinationID string `json:"destination_id"`
		Name          string `json:"name"`
		Priority      int    `json:"priority"`
		LogicOperator string `json:"logic_operator"`
		Conditions    []any  `json:"conditions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	condJSON, _ := json.Marshal(input.Conditions)
	if input.LogicOperator == "" {
		input.LogicOperator = "AND"
	}
	if input.Priority == 0 {
		input.Priority = 100
	}

	rule := &store.RoutingRule{
		SourceID:      sourceID,
		DestinationID: input.DestinationID,
		Name:          input.Name,
		Priority:      input.Priority,
		LogicOperator: input.LogicOperator,
		Conditions:    condJSON,
		IsActive:      true,
	}

	id, err := s.pg.CreateRoutingRule(r.Context(), rule)
	if err != nil {
		http.Error(w, `{"error":"failed to create rule"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	rules, err := s.pg.GetRoutingRules(r.Context(), sourceID)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch rules"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(rules)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	var input struct {
		Name          string `json:"name"`
		Priority      int    `json:"priority"`
		LogicOperator string `json:"logic_operator"`
		Conditions    []any  `json:"conditions"`
		IsActive      bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	condJSON, _ := json.Marshal(input.Conditions)
	rule := &store.RoutingRule{
		Name:          input.Name,
		Priority:      input.Priority,
		LogicOperator: input.LogicOperator,
		Conditions:    condJSON,
		IsActive:      input.IsActive,
	}

	if err := s.pg.UpdateRoutingRule(r.Context(), ruleID, rule); err != nil {
		http.Error(w, `{"error":"failed to update rule"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	ruleID := chi.URLParam(r, "ruleID")
	if err := s.pg.DeleteRoutingRule(r.Context(), ruleID); err != nil {
		http.Error(w, `{"error":"failed to delete rule"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (s *Server) handleTestRule(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"not implemented"}`, http.StatusNotImplemented)
}

// --- Replay ---

func (s *Server) handleStartReplay(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	var input struct {
		DestinationURL string `json:"destination_url"`
		FromTimestamp  string `json:"from_timestamp"`
		InitiatedBy    string `json:"initiated_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	session := &store.ReplaySession{
		SourceID:       sourceID,
		DestinationURL: input.DestinationURL,
		Status:         "running",
		InitiatedBy:    input.InitiatedBy,
	}

	t, err := time.Parse(time.RFC3339, input.FromTimestamp)
	if err != nil {
		http.Error(w, `{"error":"invalid from_timestamp, use RFC3339 format"}`, http.StatusBadRequest)
		return
	}
	session.FromTimestamp = t

	id, err := s.pg.CreateReplaySession(r.Context(), session)
	if err != nil {
		http.Error(w, `{"error":"failed to create replay session"}`, http.StatusInternalServerError)
		return
	}

	// Start replay in background goroutine (use background context, not request context).
	if s.replayEngine != nil {
		go s.replayEngine.StartReplay(context.Background(), id)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "running"})
}

func (s *Server) handleGetReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	session, err := s.pg.GetReplaySession(r.Context(), sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(session)
}

func (s *Server) handlePauseReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if err := s.pg.UpdateReplaySessionStatus(r.Context(), sessionID, "paused"); err != nil {
		http.Error(w, `{"error":"failed to pause"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
}

func (s *Server) handleResumeReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if err := s.pg.UpdateReplaySessionStatus(r.Context(), sessionID, "running"); err != nil {
		http.Error(w, `{"error":"failed to resume"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "running"})
}

func (s *Server) handleCancelReplay(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if err := s.pg.UpdateReplaySessionStatus(r.Context(), sessionID, "cancelled"); err != nil {
		http.Error(w, `{"error":"failed to cancel"}`, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// --- Helpers ---

func queryInt(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

