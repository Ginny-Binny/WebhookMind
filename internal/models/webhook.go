package models

import "time"

// WebhookEvent is the canonical event object that flows through the entire pipeline.
type WebhookEvent struct {
	ID         string            `json:"id"`
	SourceID   string            `json:"source_id"`
	ReceivedAt time.Time         `json:"received_at"`
	RawBody    []byte            `json:"raw_body"`
	Headers    map[string]string `json:"headers"`
	// Phase 2: File processing fields
	FileURL       string         `json:"file_url,omitempty"`
	FileStorePath string         `json:"file_store_path,omitempty"`
	ExtractedData map[string]any `json:"extracted_data,omitempty"`
	ExtractionMs  int64          `json:"extraction_ms,omitempty"`
}

// DeliveryAttempt represents one attempt to deliver to one destination.
type DeliveryAttempt struct {
	ID            string    `json:"id"`
	EventID       string    `json:"event_id"`
	SourceID      string    `json:"source_id"`
	DestinationID string    `json:"destination_id"`
	AttemptNumber int       `json:"attempt_number"`
	AttemptedAt   time.Time `json:"attempted_at"`
	StatusCode    int       `json:"status_code"`
	Success       bool      `json:"success"`
	ErrorMessage  string    `json:"error_message"`
	DurationMs    int64     `json:"duration_ms"`
}

// Destination represents a configured delivery target.
type Destination struct {
	ID             string    `json:"id"`
	SourceID       string    `json:"source_id"`
	Name           string    `json:"name"`
	URL            string    `json:"url"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
}

// DeadLetterEntry represents an event that exhausted all delivery retries.
type DeadLetterEntry struct {
	ID            string            `json:"id"`
	EventID       string            `json:"event_id"`
	SourceID      string            `json:"source_id"`
	DestinationID string            `json:"destination_id"`
	RawBody       []byte            `json:"raw_body"`
	Headers       map[string]string `json:"headers"`
	FailedAt      time.Time         `json:"failed_at"`
	FailureReason string            `json:"failure_reason"`
	Resolved      bool              `json:"resolved"`
	ResolvedAt    *time.Time        `json:"resolved_at,omitempty"`
}

// Source represents a registered webhook sender.
type Source struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
