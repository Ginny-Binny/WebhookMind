package models

import "time"

// ExtractionRecord tracks every file extraction attempt.
type ExtractionRecord struct {
	ID            string         `json:"id"`
	EventID       string         `json:"event_id"`
	SourceID      string         `json:"source_id"`
	FileURL       string         `json:"file_url"`
	MinIOPath     string         `json:"minio_path"`
	FileType      string         `json:"file_type"`
	TemplateID    string         `json:"template_id,omitempty"`
	CacheHit      bool           `json:"cache_hit"`
	ExtractedData map[string]any `json:"extracted_data,omitempty"`
	DurationMs    int64          `json:"duration_ms"`
	Success       bool           `json:"success"`
	ErrorMessage  string         `json:"error_message,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Template represents a document fingerprint template for fast extraction.
type Template struct {
	TemplateID      string         `json:"template_id"`
	SourceID        string         `json:"source_id"`
	FileType        string         `json:"file_type"`
	FieldPositionMap map[string]any `json:"field_position_map"`
	SampleEventID   string         `json:"sample_event_id,omitempty"`
	ConfidenceScore float64        `json:"confidence_score"`
	UseCount        int64          `json:"use_count"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}
