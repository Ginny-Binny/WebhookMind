package schema

import (
	"context"
	"log/slog"
	"time"
)

// DriftStore defines the postgres methods needed by drift detection.
type DriftStore interface {
	SchemaStore
	InsertDriftEvent(ctx context.Context, event *DriftEvent) error
}

// CheckDrift validates a payload against a locked schema and records drift events.
func CheckDrift(ctx context.Context, store DriftStore, logger *slog.Logger, sourceID, eventID string, payload map[string]any) []DriftEvent {
	schema, err := store.GetPayloadSchema(ctx, sourceID)
	if err != nil || !schema.IsLocked {
		return nil
	}

	flat := FlattenJSON(payload)
	var drifts []DriftEvent

	// Check for missing required fields.
	for name, field := range schema.Fields {
		if field.Nullable {
			continue
		}
		if _, present := flat[name]; !present {
			drift := DriftEvent{
				EventID:      eventID,
				SourceID:     sourceID,
				DriftType:    "FIELD_MISSING",
				FieldName:    name,
				ExpectedType: field.Type,
				DetectedAt:   time.Now().UTC(),
			}
			drifts = append(drifts, drift)
		}
	}

	// Check for type changes and new fields.
	for name, value := range flat {
		actualType := DetectType(value)

		if field, exists := schema.Fields[name]; exists {
			if field.Type != actualType && actualType != "null" {
				drift := DriftEvent{
					EventID:      eventID,
					SourceID:     sourceID,
					DriftType:    "TYPE_CHANGED",
					FieldName:    name,
					ExpectedType: field.Type,
					ActualType:   actualType,
					DetectedAt:   time.Now().UTC(),
				}
				drifts = append(drifts, drift)
			}
		} else {
			drift := DriftEvent{
				EventID:    eventID,
				SourceID:   sourceID,
				DriftType:  "NEW_FIELD",
				FieldName:  name,
				ActualType: actualType,
				DetectedAt: time.Now().UTC(),
			}
			drifts = append(drifts, drift)
		}
	}

	// Persist drift events.
	for i := range drifts {
		if err := store.InsertDriftEvent(ctx, &drifts[i]); err != nil {
			logger.Error("failed to insert drift event",
				"source_id", sourceID,
				"event_id", eventID,
				"error", err,
			)
		}
	}

	if len(drifts) > 0 {
		logger.Warn("schema drift detected",
			"source_id", sourceID,
			"event_id", eventID,
			"drift_count", len(drifts),
		)
	}

	return drifts
}
