package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// SchemaStore defines the postgres methods needed by the schema engine.
type SchemaStore interface {
	GetPayloadSchema(ctx context.Context, sourceID string) (*PayloadSchema, error)
	UpsertPayloadSchema(ctx context.Context, schema *PayloadSchema) error
}

// UpdateSchema accumulates field observations and locks the schema after minSamples.
func UpdateSchema(ctx context.Context, store SchemaStore, logger *slog.Logger, sourceID string, payload map[string]any, minSamples int) {
	flat := FlattenJSON(payload)

	existing, err := store.GetPayloadSchema(ctx, sourceID)
	if err != nil {
		// First webhook from this source — create new schema.
		existing = &PayloadSchema{
			SourceID:    sourceID,
			Fields:      make(map[string]FieldSchema),
			SampleCount: 0,
			Version:     1,
		}
	}

	if existing.IsLocked {
		// Schema already locked, don't update it.
		return
	}

	existing.SampleCount++

	// Mark all existing fields as potentially nullable if absent in this payload.
	for name, field := range existing.Fields {
		if _, present := flat[name]; !present {
			field.Nullable = true
			existing.Fields[name] = field
		}
	}

	// Process current payload fields.
	for name, value := range flat {
		fieldType := DetectType(value)

		if field, exists := existing.Fields[name]; exists {
			// Update type if different (take latest).
			if field.Type != fieldType && fieldType != "null" {
				field.Type = fieldType
			}
			// Add example (up to 3).
			if len(field.Examples) < 3 {
				field.Examples = append(field.Examples, truncateExample(value))
			}
			existing.Fields[name] = field
		} else {
			// New field.
			nullable := existing.SampleCount > 1 // absent in previous samples
			existing.Fields[name] = FieldSchema{
				Name:     name,
				Type:     fieldType,
				Nullable: nullable,
				Examples: []any{truncateExample(value)},
			}
		}
	}

	// Lock schema after enough samples.
	if existing.SampleCount >= minSamples && !existing.IsLocked {
		existing.IsLocked = true
		existing.InferredAt = time.Now().UTC()
		logger.Info("schema locked",
			"source_id", sourceID,
			"sample_count", existing.SampleCount,
			"field_count", len(existing.Fields),
		)
	}

	if err := store.UpsertPayloadSchema(ctx, existing); err != nil {
		logger.Error("failed to upsert schema",
			"source_id", sourceID,
			"error", err,
		)
	}
}

func truncateExample(v any) any {
	if s, ok := v.(string); ok && len(s) > 100 {
		return s[:100] + "..."
	}
	// Don't store large arrays/objects as examples.
	if _, ok := v.([]any); ok {
		return "[array]"
	}
	if _, ok := v.(map[string]any); ok {
		return "[object]"
	}
	return v
}

// MarshalSchema converts a PayloadSchema to JSON bytes for DB storage.
func MarshalSchema(schema *PayloadSchema) ([]byte, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return data, nil
}
