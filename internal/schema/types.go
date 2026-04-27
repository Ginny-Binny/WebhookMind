package schema

import "time"

type FieldSchema struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "string" | "number" | "boolean" | "object" | "array" | "null"
	Nullable bool   `json:"nullable"` // was it absent in any observed webhook?
	Examples []any  `json:"examples"` // up to 3 sample values
}

type PayloadSchema struct {
	SourceID    string                 `json:"source_id"`
	Fields      map[string]FieldSchema `json:"fields"`
	SampleCount int                    `json:"sample_count"`
	InferredAt  time.Time              `json:"inferred_at"`
	Version     int                    `json:"version"`
	IsLocked    bool                   `json:"is_locked"`
}

type DriftEvent struct {
	ID           string    `json:"id"`
	EventID      string    `json:"event_id"`
	SourceID     string    `json:"source_id"`
	DriftType    string    `json:"drift_type"` // FIELD_MISSING | TYPE_CHANGED | NEW_FIELD
	FieldName    string    `json:"field_name"`
	ExpectedType string    `json:"expected_type"`
	ActualType   string    `json:"actual_type"`
	Details      any       `json:"details,omitempty"`
	DetectedAt   time.Time `json:"detected_at"`
}
