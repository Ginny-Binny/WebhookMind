package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/models"
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

func (s *PostgresStore) Close() {
	s.pool.Close()
}
