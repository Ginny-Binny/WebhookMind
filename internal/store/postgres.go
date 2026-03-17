package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
	"github.com/gauravfs-14/webhookmind/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) GetDestinationsBySourceID(ctx context.Context, sourceID string) ([]models.Destination, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, source_id, name, url, timeout_seconds, is_active, created_at
		 FROM destinations
		 WHERE source_id = $1 AND is_active = TRUE`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query destinations: %w", err)
	}
	defer rows.Close()

	var dests []models.Destination
	for rows.Next() {
		var d models.Destination
		if err := rows.Scan(&d.ID, &d.SourceID, &d.Name, &d.URL, &d.TimeoutSeconds, &d.IsActive, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan destination: %w", err)
		}
		dests = append(dests, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate destinations: %w", err)
	}

	return dests, nil
}

func (s *PostgresStore) InsertDeliveryAttempt(ctx context.Context, attempt *models.DeliveryAttempt) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO delivery_attempts (event_id, source_id, destination_id, attempt_number, attempted_at, status_code, success, error_message, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		attempt.EventID,
		attempt.SourceID,
		attempt.DestinationID,
		attempt.AttemptNumber,
		attempt.AttemptedAt,
		attempt.StatusCode,
		attempt.Success,
		attempt.ErrorMessage,
		attempt.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("insert delivery attempt: %w", err)
	}
	return nil
}

func (s *PostgresStore) InsertDeadLetterEntry(ctx context.Context, entry *models.DeadLetterEntry) error {
	headersJSON, err := json.Marshal(entry.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO dead_letter_queue (event_id, source_id, destination_id, raw_body, headers, failed_at, failure_reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.EventID,
		entry.SourceID,
		entry.DestinationID,
		entry.RawBody,
		headersJSON,
		entry.FailedAt,
		entry.FailureReason,
	)
	if err != nil {
		return fmt.Errorf("insert dead letter entry: %w", err)
	}
	return nil
}

// IncompleteDelivery holds info needed to re-enqueue a failed delivery on restart.
type IncompleteDelivery struct {
	EventID       string
	SourceID      string
	DestinationID string
	AttemptNumber int
	LastAttemptAt time.Time
}

func (s *PostgresStore) GetIncompleteDeliveries(ctx context.Context) ([]IncompleteDelivery, error) {
	// Find the latest attempt per (event_id, destination_id) that failed
	// and still has retries remaining. Only re-enqueue if enough backoff time has elapsed.
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (event_id, destination_id)
		        event_id, source_id, destination_id, attempt_number, attempted_at
		 FROM delivery_attempts
		 WHERE success = FALSE
		   AND attempt_number < $1
		   AND event_id NOT IN (
		       SELECT event_id FROM delivery_attempts WHERE success = TRUE
		   )
		   AND event_id NOT IN (
		       SELECT event_id FROM dead_letter_queue
		   )
		 ORDER BY event_id, destination_id, attempt_number DESC`, 4)
	if err != nil {
		return nil, fmt.Errorf("query incomplete deliveries: %w", err)
	}
	defer rows.Close()

	var results []IncompleteDelivery
	for rows.Next() {
		var d IncompleteDelivery
		if err := rows.Scan(&d.EventID, &d.SourceID, &d.DestinationID, &d.AttemptNumber, &d.LastAttemptAt); err != nil {
			return nil, fmt.Errorf("scan incomplete delivery: %w", err)
		}
		results = append(results, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incomplete deliveries: %w", err)
	}

	return results, nil
}

func (s *PostgresStore) InsertExtractionRecord(ctx context.Context, record *models.ExtractionRecord) error {
	extractedJSON, err := json.Marshal(record.ExtractedData)
	if err != nil {
		return fmt.Errorf("marshal extracted data: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO extraction_records (event_id, source_id, file_url, minio_path, file_type, template_id, cache_hit, extracted_data, duration_ms, success, error_message)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		record.EventID,
		record.SourceID,
		record.FileURL,
		record.MinIOPath,
		record.FileType,
		record.TemplateID,
		record.CacheHit,
		extractedJSON,
		record.DurationMs,
		record.Success,
		record.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert extraction record: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpsertTemplate(ctx context.Context, tmpl *models.Template) error {
	fieldMapJSON, err := json.Marshal(tmpl.FieldPositionMap)
	if err != nil {
		return fmt.Errorf("marshal field position map: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO templates (template_id, source_id, file_type, field_position_map, sample_event_id, confidence_score)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (template_id) DO UPDATE SET
		   use_count = templates.use_count + 1,
		   updated_at = NOW()`,
		tmpl.TemplateID,
		tmpl.SourceID,
		tmpl.FileType,
		fieldMapJSON,
		tmpl.SampleEventID,
		tmpl.ConfidenceScore,
	)
	if err != nil {
		return fmt.Errorf("upsert template: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetTemplate(ctx context.Context, templateID string) (*models.Template, error) {
	var tmpl models.Template
	var fieldMapJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT template_id, source_id, file_type, field_position_map, sample_event_id, confidence_score, use_count, created_at, updated_at
		 FROM templates WHERE template_id = $1`, templateID).
		Scan(&tmpl.TemplateID, &tmpl.SourceID, &tmpl.FileType, &fieldMapJSON, &tmpl.SampleEventID, &tmpl.ConfidenceScore, &tmpl.UseCount, &tmpl.CreatedAt, &tmpl.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	if err := json.Unmarshal(fieldMapJSON, &tmpl.FieldPositionMap); err != nil {
		return nil, fmt.Errorf("unmarshal field position map: %w", err)
	}

	return &tmpl, nil
}

// --- Schema Inference ---

func (s *PostgresStore) GetPayloadSchema(ctx context.Context, sourceID string) (*schema.PayloadSchema, error) {
	var schemaData []byte
	var ps schema.PayloadSchema
	err := s.pool.QueryRow(ctx,
		`SELECT source_id, schema_data, sample_count, version, is_locked, created_at, updated_at
		 FROM payload_schemas WHERE source_id = $1`, sourceID).
		Scan(&ps.SourceID, &schemaData, &ps.SampleCount, &ps.Version, &ps.IsLocked, &ps.InferredAt, &ps.InferredAt)
	if err != nil {
		return nil, fmt.Errorf("get payload schema: %w", err)
	}
	if err := json.Unmarshal(schemaData, &ps.Fields); err != nil {
		return nil, fmt.Errorf("unmarshal schema fields: %w", err)
	}
	return &ps, nil
}

func (s *PostgresStore) UpsertPayloadSchema(ctx context.Context, ps *schema.PayloadSchema) error {
	schemaData, err := json.Marshal(ps.Fields)
	if err != nil {
		return fmt.Errorf("marshal schema fields: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO payload_schemas (source_id, schema_data, sample_count, version, is_locked, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (source_id) DO UPDATE SET
		   schema_data = $2, sample_count = $3, version = $4, is_locked = $5, updated_at = NOW()`,
		ps.SourceID, schemaData, ps.SampleCount, ps.Version, ps.IsLocked)
	if err != nil {
		return fmt.Errorf("upsert payload schema: %w", err)
	}
	return nil
}

func (s *PostgresStore) InsertDriftEvent(ctx context.Context, event *schema.DriftEvent) error {
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		detailsJSON = []byte("{}")
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO drift_events (event_id, source_id, drift_type, field_name, expected_type, actual_type, details)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.EventID, event.SourceID, event.DriftType, event.FieldName, event.ExpectedType, event.ActualType, detailsJSON)
	if err != nil {
		return fmt.Errorf("insert drift event: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDriftEvents(ctx context.Context, sourceID string, limit int) ([]schema.DriftEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, event_id, source_id, drift_type, field_name, expected_type, actual_type, detected_at
		 FROM drift_events WHERE source_id = $1 ORDER BY detected_at DESC LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query drift events: %w", err)
	}
	defer rows.Close()

	var events []schema.DriftEvent
	for rows.Next() {
		var e schema.DriftEvent
		if err := rows.Scan(&e.ID, &e.EventID, &e.SourceID, &e.DriftType, &e.FieldName, &e.ExpectedType, &e.ActualType, &e.DetectedAt); err != nil {
			return nil, fmt.Errorf("scan drift event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Webhook Diffs ---

func (s *PostgresStore) InsertWebhookDiff(ctx context.Context, eventID, sourceID, prevEventID string, diffData []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_diffs (event_id, source_id, prev_event_id, diff_data)
		 VALUES ($1, $2, $3, $4)`,
		eventID, sourceID, prevEventID, diffData)
	if err != nil {
		return fmt.Errorf("insert webhook diff: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPreviousEventID(ctx context.Context, sourceID, currentEventID string) (string, error) {
	var eventID string
	err := s.pool.QueryRow(ctx,
		`SELECT event_id FROM delivery_attempts
		 WHERE source_id = $1 AND success = true AND event_id != $2
		 ORDER BY attempted_at DESC LIMIT 1`, sourceID, currentEventID).
		Scan(&eventID)
	if err != nil {
		return "", fmt.Errorf("get previous event: %w", err)
	}
	return eventID, nil
}

func (s *PostgresStore) GetWebhookDiffs(ctx context.Context, sourceID string, limit int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, event_id, prev_event_id, diff_data, computed_at
		 FROM webhook_diffs WHERE source_id = $1 ORDER BY computed_at DESC LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query webhook diffs: %w", err)
	}
	defer rows.Close()

	var diffs []map[string]any
	for rows.Next() {
		var id, eventID string
		var prevEventID *string
		var diffData []byte
		var computedAt time.Time
		if err := rows.Scan(&id, &eventID, &prevEventID, &diffData, &computedAt); err != nil {
			return nil, fmt.Errorf("scan webhook diff: %w", err)
		}
		var parsed map[string]any
		json.Unmarshal(diffData, &parsed)
		diffs = append(diffs, map[string]any{
			"id":            id,
			"event_id":      eventID,
			"prev_event_id": prevEventID,
			"diff_data":     parsed,
			"computed_at":   computedAt,
		})
	}
	return diffs, rows.Err()
}

// --- Routing Rules ---

type RoutingRule struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	DestinationID string `json:"destination_id"`
	Name          string `json:"name"`
	Priority      int    `json:"priority"`
	LogicOperator string `json:"logic_operator"`
	Conditions    []byte `json:"conditions"`
	IsActive      bool   `json:"is_active"`
}

func (s *PostgresStore) GetRoutingRules(ctx context.Context, sourceID string) ([]RoutingRule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, source_id, destination_id, name, priority, logic_operator, conditions, is_active
		 FROM routing_rules WHERE source_id = $1 AND is_active = true ORDER BY priority ASC`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query routing rules: %w", err)
	}
	defer rows.Close()

	var rules []RoutingRule
	for rows.Next() {
		var r RoutingRule
		if err := rows.Scan(&r.ID, &r.SourceID, &r.DestinationID, &r.Name, &r.Priority, &r.LogicOperator, &r.Conditions, &r.IsActive); err != nil {
			return nil, fmt.Errorf("scan routing rule: %w", err)
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *PostgresStore) CreateRoutingRule(ctx context.Context, r *RoutingRule) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO routing_rules (source_id, destination_id, name, priority, logic_operator, conditions)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		r.SourceID, r.DestinationID, r.Name, r.Priority, r.LogicOperator, r.Conditions).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create routing rule: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) UpdateRoutingRule(ctx context.Context, ruleID string, r *RoutingRule) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE routing_rules SET name = $2, priority = $3, logic_operator = $4, conditions = $5, is_active = $6
		 WHERE id = $1`,
		ruleID, r.Name, r.Priority, r.LogicOperator, r.Conditions, r.IsActive)
	if err != nil {
		return fmt.Errorf("update routing rule: %w", err)
	}
	return nil
}

func (s *PostgresStore) DeleteRoutingRule(ctx context.Context, ruleID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM routing_rules WHERE id = $1`, ruleID)
	if err != nil {
		return fmt.Errorf("delete routing rule: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetDefaultDestinations(ctx context.Context, sourceID string) ([]models.Destination, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, source_id, name, url, timeout_seconds, is_active, created_at
		 FROM destinations WHERE source_id = $1 AND is_active = TRUE AND is_default = TRUE`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query default destinations: %w", err)
	}
	defer rows.Close()

	var dests []models.Destination
	for rows.Next() {
		var d models.Destination
		if err := rows.Scan(&d.ID, &d.SourceID, &d.Name, &d.URL, &d.TimeoutSeconds, &d.IsActive, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan destination: %w", err)
		}
		dests = append(dests, d)
	}
	return dests, rows.Err()
}

// --- Replay Sessions ---

type ReplaySession struct {
	ID              string     `json:"id"`
	SourceID        string     `json:"source_id"`
	DestinationURL  string     `json:"destination_url"`
	FromTimestamp   time.Time  `json:"from_timestamp"`
	ToTimestamp     *time.Time `json:"to_timestamp,omitempty"`
	Status          string     `json:"status"`
	EventsReplayed  int        `json:"events_replayed"`
	EventsTotal     *int       `json:"events_total,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	InitiatedBy     string     `json:"initiated_by,omitempty"`
}

func (s *PostgresStore) CreateReplaySession(ctx context.Context, rs *ReplaySession) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO replay_sessions (source_id, destination_url, from_timestamp, to_timestamp, status, initiated_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		rs.SourceID, rs.DestinationURL, rs.FromTimestamp, rs.ToTimestamp, rs.Status, rs.InitiatedBy).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create replay session: %w", err)
	}
	return id, nil
}

func (s *PostgresStore) GetReplaySession(ctx context.Context, sessionID string) (*ReplaySession, error) {
	var rs ReplaySession
	err := s.pool.QueryRow(ctx,
		`SELECT id, source_id, destination_url, from_timestamp, to_timestamp, status, events_replayed, events_total, started_at, completed_at, initiated_by
		 FROM replay_sessions WHERE id = $1`, sessionID).
		Scan(&rs.ID, &rs.SourceID, &rs.DestinationURL, &rs.FromTimestamp, &rs.ToTimestamp, &rs.Status, &rs.EventsReplayed, &rs.EventsTotal, &rs.StartedAt, &rs.CompletedAt, &rs.InitiatedBy)
	if err != nil {
		return nil, fmt.Errorf("get replay session: %w", err)
	}
	return &rs, nil
}

func (s *PostgresStore) UpdateReplaySessionStatus(ctx context.Context, sessionID, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE replay_sessions SET status = $2, completed_at = CASE WHEN $2 IN ('completed', 'failed') THEN NOW() ELSE completed_at END
		 WHERE id = $1`, sessionID, status)
	if err != nil {
		return fmt.Errorf("update replay session status: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateReplaySessionProgress(ctx context.Context, sessionID string, replayed, total int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE replay_sessions SET events_replayed = $2, events_total = $3 WHERE id = $1`,
		sessionID, replayed, total)
	if err != nil {
		return fmt.Errorf("update replay session progress: %w", err)
	}
	return nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}
