package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/gauravfs-14/webhookmind/internal/routing"
	"github.com/gauravfs-14/webhookmind/internal/schema"
	"github.com/gauravfs-14/webhookmind/internal/store"
	"github.com/google/uuid"
)

// Backoff delays between attempts. Index 0 = before attempt 1 (immediate),
// index 1 = before attempt 2 (5s), etc.
var retryDelays = []time.Duration{
	0,
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
}

type Engine struct {
	queue            *queue.RedisQueue
	pg               *store.PostgresStore
	scylla           *store.ScyllaStore
	client           *http.Client
	logger           *slog.Logger
	maxRetries       int
	schemaMinSamples int
}

func NewEngine(
	q *queue.RedisQueue,
	pg *store.PostgresStore,
	scylla *store.ScyllaStore,
	logger *slog.Logger,
	maxRetries int,
	schemaMinSamples int,
) *Engine {
	return &Engine{
		queue:            q,
		pg:               pg,
		scylla:           scylla,
		client:           &http.Client{},
		logger:           logger,
		maxRetries:       maxRetries,
		schemaMinSamples: schemaMinSamples,
	}
}

func (e *Engine) Run(ctx context.Context, workerCount int) *sync.WaitGroup {
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			e.logger.Debug("delivery worker started", "worker_id", workerID)

			for {
				select {
				case <-ctx.Done():
					e.logger.Debug("delivery worker stopping", "worker_id", workerID)
					return
				default:
				}

				event, err := e.queue.Dequeue(ctx, queue.QueueDelivery, 5*time.Second)
				if err != nil {
					e.logger.Error("dequeue failed",
						"worker_id", workerID,
						"error", err,
					)
					continue
				}

				if event == nil {
					continue
				}

				e.deliverEvent(ctx, event)
			}
		}(i)
	}

	return &wg
}

func (e *Engine) deliverEvent(ctx context.Context, event *models.WebhookEvent) {
	// Write event to ScyllaDB immediately so it's available for restart recovery.
	if err := e.scylla.InsertEvent(event); err != nil {
		e.logger.Error("failed to write event to scylla",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error", err,
		)
	}

	// Parse payload for schema inference, drift detection, and routing.
	var payload map[string]any
	if err := json.Unmarshal(event.RawBody, &payload); err == nil {
		// Schema inference (runs for all webhooks, fast).
		schema.UpdateSchema(ctx, e.pg, e.logger, event.SourceID, payload, e.schemaMinSamples)

		// Drift detection (only after schema is locked).
		schema.CheckDrift(ctx, e.pg, e.logger, event.SourceID, event.ID, payload)

		// Async diff against previous webhook (never blocks delivery).
		// Small delay to let current delivery record first.
		go func() {
			time.Sleep(2 * time.Second)
			e.computeDiffAsync(ctx, event, payload)
		}()
	}

	// Resolve destinations via routing rules (falls back to defaults).
	dests, err := routing.ResolveDestinations(ctx, e.pg, event.SourceID, event.RawBody, e.logger)
	if err != nil {
		e.logger.Error("failed to resolve destinations",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error", err,
		)
		return
	}

	if len(dests) == 0 {
		e.logger.Warn("no active destinations for source",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
		)
		return
	}

	// Fan-out: deliver to each destination concurrently.
	var wg sync.WaitGroup
	for _, dest := range dests {
		wg.Add(1)
		go func(d models.Destination) {
			defer wg.Done()
			e.deliverToDestination(ctx, event, d)
		}(dest)
	}
	wg.Wait()
}

func (e *Engine) deliverToDestination(ctx context.Context, event *models.WebhookEvent, dest models.Destination) {
	for attempt := 1; attempt <= e.maxRetries; attempt++ {
		// Wait for backoff delay (0 for first attempt).
		if attempt > 1 {
			delay := retryDelays[attempt-1]
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				e.logger.Info("delivery cancelled during backoff",
					"component", "delivery",
					"event_id", event.ID,
					"source_id", event.SourceID,
					"destination_id", dest.ID,
					"attempt", attempt,
				)
				return
			}
		}

		statusCode, duration, err := e.attemptDelivery(ctx, event, dest)

		attemptRecord := &models.DeliveryAttempt{
			ID:            uuid.New().String(),
			EventID:       event.ID,
			SourceID:      event.SourceID,
			DestinationID: dest.ID,
			AttemptNumber: attempt,
			AttemptedAt:   time.Now().UTC(),
			StatusCode:    statusCode,
			Success:       err == nil && statusCode >= 200 && statusCode < 300,
			DurationMs:    duration.Milliseconds(),
		}

		if err != nil {
			attemptRecord.ErrorMessage = err.Error()
		}

		// Record attempt in PostgreSQL.
		if pgErr := e.pg.InsertDeliveryAttempt(ctx, attemptRecord); pgErr != nil {
			e.logger.Error("failed to record delivery attempt",
				"component", "delivery",
				"event_id", event.ID,
				"source_id", event.SourceID,
				"destination_id", dest.ID,
				"attempt", attempt,
				"error", pgErr,
			)
		}

		// Success — delivered.
		if attemptRecord.Success {
			e.logger.Info("delivery succeeded",
				"component", "delivery",
				"event_id", event.ID,
				"source_id", event.SourceID,
				"destination_id", dest.ID,
				"attempt", attempt,
				"status_code", statusCode,
				"duration_ms", duration.Milliseconds(),
			)
			return
		}

		// 4xx — permanent failure, do not retry.
		if statusCode >= 400 && statusCode < 500 {
			e.logger.Warn("delivery permanently failed (4xx)",
				"component", "delivery",
				"event_id", event.ID,
				"source_id", event.SourceID,
				"destination_id", dest.ID,
				"attempt", attempt,
				"status_code", statusCode,
			)
			e.moveToDLQ(ctx, event, dest, fmt.Sprintf("permanent failure: HTTP %d", statusCode))
			return
		}

		// Log retry.
		e.logger.Warn("delivery failed, will retry",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"destination_id", dest.ID,
			"attempt", attempt,
			"status_code", statusCode,
			"error", err,
		)
	}

	// All retries exhausted.
	e.logger.Error("delivery exhausted all retries",
		"component", "delivery",
		"event_id", event.ID,
		"source_id", event.SourceID,
		"destination_id", dest.ID,
		"max_retries", e.maxRetries,
	)
	e.moveToDLQ(ctx, event, dest, fmt.Sprintf("exhausted %d delivery attempts", e.maxRetries))
}

func (e *Engine) attemptDelivery(ctx context.Context, event *models.WebhookEvent, dest models.Destination) (statusCode int, duration time.Duration, err error) {
	start := time.Now()

	deliveryCtx, cancel := context.WithTimeout(ctx, time.Duration(dest.TimeoutSeconds)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(deliveryCtx, http.MethodPost, dest.URL, bytes.NewReader(event.RawBody))
	if err != nil {
		return 0, time.Since(start), fmt.Errorf("create request: %w", err)
	}

	// Copy original headers.
	for key, value := range event.Headers {
		req.Header.Set(key, value)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return 0, time.Since(start), fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		e.logger.Error("failed to drain response body",
			"component", "delivery",
			"event_id", event.ID,
			"error", err,
		)
	}

	return resp.StatusCode, time.Since(start), nil
}

func (e *Engine) moveToDLQ(ctx context.Context, event *models.WebhookEvent, dest models.Destination, reason string) {
	// Push to Redis DLQ.
	if err := e.queue.Enqueue(ctx, queue.QueueDLQ, event); err != nil {
		e.logger.Error("failed to push to redis DLQ",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"destination_id", dest.ID,
			"error", err,
		)
	}

	// Insert into PostgreSQL dead_letter_queue.
	entry := &models.DeadLetterEntry{
		ID:            uuid.New().String(),
		EventID:       event.ID,
		SourceID:      event.SourceID,
		DestinationID: dest.ID,
		RawBody:       event.RawBody,
		Headers:       event.Headers,
		FailedAt:      time.Now().UTC(),
		FailureReason: reason,
	}

	if err := e.pg.InsertDeadLetterEntry(ctx, entry); err != nil {
		e.logger.Error("failed to insert dead letter entry",
			"component", "delivery",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"destination_id", dest.ID,
			"error", err,
		)
	}
}

func (e *Engine) computeDiffAsync(ctx context.Context, event *models.WebhookEvent, currentPayload map[string]any) {
	prevEventID, err := e.pg.GetPreviousEventID(ctx, event.SourceID, event.ID)
	if err != nil {
		e.logger.Debug("no previous event for diff", "event_id", event.ID, "error", err)
		return
	}

	prevEvent, err := e.scylla.GetEvent(event.SourceID, prevEventID)
	if err != nil || prevEvent == nil {
		e.logger.Debug("failed to get previous event from scylla", "event_id", event.ID, "prev_event_id", prevEventID, "error", err)
		return
	}

	var prevPayload map[string]any
	if err := json.Unmarshal(prevEvent.RawBody, &prevPayload); err != nil {
		return
	}

	flat1 := schema.FlattenJSON(currentPayload)
	flat2 := schema.FlattenJSON(prevPayload)

	added := make(map[string]any)
	removed := make(map[string]any)
	var changed []map[string]any

	for k, v := range flat1 {
		if pv, exists := flat2[k]; !exists {
			added[k] = v
		} else if fmt.Sprintf("%v", v) != fmt.Sprintf("%v", pv) {
			changed = append(changed, map[string]any{
				"field":     k,
				"old_value": pv,
				"new_value": v,
			})
		}
	}
	for k, v := range flat2 {
		if _, exists := flat1[k]; !exists {
			removed[k] = v
		}
	}

	diffData, _ := json.Marshal(map[string]any{
		"added":   added,
		"removed": removed,
		"changed": changed,
	})

	if err := e.pg.InsertWebhookDiff(ctx, event.ID, event.SourceID, prevEventID, diffData); err != nil {
		e.logger.Error("failed to insert webhook diff",
			"event_id", event.ID,
			"error", err,
		)
	}
}

// RecoverIncomplete re-enqueues events that were in-progress when the process last stopped.
func (e *Engine) RecoverIncomplete(ctx context.Context) {
	incomplete, err := e.pg.GetIncompleteDeliveries(ctx)
	if err != nil {
		e.logger.Error("failed to query incomplete deliveries",
			"component", "delivery",
			"error", err,
		)
		return
	}

	if len(incomplete) == 0 {
		e.logger.Info("no incomplete deliveries to recover", "component", "delivery")
		return
	}

	recovered := 0
	for _, d := range incomplete {
		// Check if enough backoff time has elapsed.
		nextDelay := retryDelays[0]
		if d.AttemptNumber < len(retryDelays) {
			nextDelay = retryDelays[d.AttemptNumber]
		}
		if time.Since(d.LastAttemptAt) < nextDelay {
			continue
		}

		// Fetch event from ScyllaDB.
		event, err := e.scylla.GetEvent(d.SourceID, d.EventID)
		if err != nil {
			e.logger.Error("failed to fetch event for recovery",
				"component", "delivery",
				"event_id", d.EventID,
				"source_id", d.SourceID,
				"error", err,
			)
			continue
		}

		if err := e.queue.Enqueue(ctx, queue.QueueDelivery, event); err != nil {
			e.logger.Error("failed to re-enqueue event for recovery",
				"component", "delivery",
				"event_id", d.EventID,
				"source_id", d.SourceID,
				"error", err,
			)
			continue
		}

		recovered++
		e.logger.Info("recovered incomplete delivery",
			"component", "delivery",
			"event_id", d.EventID,
			"source_id", d.SourceID,
			"destination_id", d.DestinationID,
			"previous_attempts", d.AttemptNumber,
		)
	}

	e.logger.Info("recovery complete",
		"component", "delivery",
		"recovered", recovered,
		"total_incomplete", len(incomplete),
	)
}
