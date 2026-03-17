package replay

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

type Engine struct {
	pg     *store.PostgresStore
	scylla *store.ScyllaStore
	logger *slog.Logger
	client *http.Client
}

func NewEngine(pg *store.PostgresStore, scylla *store.ScyllaStore, logger *slog.Logger) *Engine {
	return &Engine{
		pg:     pg,
		scylla: scylla,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// StartReplay runs a replay session in the current goroutine.
// Call this in a background goroutine from the API handler.
func (e *Engine) StartReplay(ctx context.Context, sessionID string) {
	session, err := e.pg.GetReplaySession(ctx, sessionID)
	if err != nil {
		e.logger.Error("failed to get replay session", "session_id", sessionID, "error", err)
		return
	}

	e.logger.Info("starting replay",
		"session_id", sessionID,
		"source_id", session.SourceID,
		"destination", session.DestinationURL,
		"from", session.FromTimestamp,
	)

	// Fetch events from ScyllaDB.
	e.logger.Info("fetching events from scylla",
		"session_id", sessionID,
		"source_id", session.SourceID,
		"since", session.FromTimestamp.UTC(),
	)
	events, err := e.scylla.GetEventsBySourceSince(session.SourceID, session.FromTimestamp)
	if err != nil {
		e.logger.Error("failed to fetch events for replay", "session_id", sessionID, "error", err)
		e.pg.UpdateReplaySessionStatus(ctx, sessionID, "failed")
		return
	}

	total := len(events)
	e.logger.Info("replay events fetched", "session_id", sessionID, "total", total)
	e.pg.UpdateReplaySessionProgress(ctx, sessionID, 0, total)

	for i, event := range events {
		// Check for pause/cancel.
		currentSession, err := e.pg.GetReplaySession(ctx, sessionID)
		if err != nil {
			break
		}
		if currentSession.Status == "paused" {
			e.logger.Info("replay paused", "session_id", sessionID, "replayed", i)
			// Wait until resumed or cancelled.
			for {
				time.Sleep(2 * time.Second)
				s, err := e.pg.GetReplaySession(ctx, sessionID)
				if err != nil || s.Status == "cancelled" {
					e.pg.UpdateReplaySessionStatus(ctx, sessionID, "cancelled")
					return
				}
				if s.Status == "running" {
					break
				}
			}
		}
		if currentSession.Status == "cancelled" {
			e.logger.Info("replay cancelled", "session_id", sessionID)
			return
		}

		// Deliver event to destination (ordered, wait for response).
		if err := e.deliverReplayEvent(ctx, event, session.DestinationURL); err != nil {
			e.logger.Error("replay delivery failed",
				"session_id", sessionID,
				"event_id", event.ID,
				"error", err,
			)
			// Continue with next event.
		}

		e.pg.UpdateReplaySessionProgress(ctx, sessionID, i+1, total)
		e.logger.Debug("replay event delivered", "session_id", sessionID, "event_id", event.ID, "progress", fmt.Sprintf("%d/%d", i+1, total))
	}

	e.pg.UpdateReplaySessionStatus(ctx, sessionID, "completed")
	e.logger.Info("replay completed",
		"session_id", sessionID,
		"events_replayed", total,
	)
}

func (e *Engine) deliverReplayEvent(ctx context.Context, event *models.WebhookEvent, destURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destURL, bytes.NewReader(event.RawBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for key, value := range event.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("X-Replay", "true")
	req.Header.Set("X-Original-Event-ID", event.ID)

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("destination returned %d", resp.StatusCode)
	}

	return nil
}
