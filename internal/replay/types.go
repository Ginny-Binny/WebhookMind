package replay

import "time"

type Session struct {
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
