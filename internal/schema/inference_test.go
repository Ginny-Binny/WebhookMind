package schema

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memSchemaStore is an in-process SchemaStore for unit testing.
type memSchemaStore struct {
	schemas map[string]*PayloadSchema // sourceID -> schema
}

func newMemSchemaStore() *memSchemaStore {
	return &memSchemaStore{schemas: make(map[string]*PayloadSchema)}
}

func (m *memSchemaStore) GetPayloadSchema(_ context.Context, sourceID string) (*PayloadSchema, error) {
	s, ok := m.schemas[sourceID]
	if !ok {
		return nil, errors.New("not found")
	}
	return s, nil
}

func (m *memSchemaStore) UpsertPayloadSchema(_ context.Context, schema *PayloadSchema) error {
	// Copy so subsequent mutations by the caller don't change stored state.
	cp := *schema
	cp.Fields = make(map[string]FieldSchema, len(schema.Fields))
	for k, v := range schema.Fields {
		cp.Fields[k] = v
	}
	m.schemas[schema.SourceID] = &cp
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestUpdateSchema_FirstCallCreatesSchema(t *testing.T) {
	store := newMemSchemaStore()
	ctx := context.Background()

	UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{
		"order_id": "ORD-1",
		"amount":   500.0,
	}, 10)

	got, err := store.GetPayloadSchema(ctx, "src-1")
	require.NoError(t, err)
	assert.Equal(t, 1, got.SampleCount)
	assert.False(t, got.IsLocked)
	assert.Equal(t, "string", got.Fields["order_id"].Type)
	assert.Equal(t, "number", got.Fields["amount"].Type)
}

func TestUpdateSchema_AccumulatesNewFields(t *testing.T) {
	store := newMemSchemaStore()
	ctx := context.Background()

	UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{"a": 1.0}, 10)
	UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{"a": 2.0, "b": "two"}, 10)

	got, err := store.GetPayloadSchema(ctx, "src-1")
	require.NoError(t, err)
	assert.Equal(t, 2, got.SampleCount)
	assert.Contains(t, got.Fields, "a")
	assert.Contains(t, got.Fields, "b")
	// "b" only showed up on the second sample, so it's nullable.
	assert.True(t, got.Fields["b"].Nullable)
}

func TestUpdateSchema_LocksAtMinSamples(t *testing.T) {
	store := newMemSchemaStore()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{"a": 1.0}, 3)
	}

	got, err := store.GetPayloadSchema(ctx, "src-1")
	require.NoError(t, err)
	assert.True(t, got.IsLocked, "schema should lock at minSamples=3")
	assert.Equal(t, 3, got.SampleCount)
	assert.False(t, got.InferredAt.IsZero(), "inferred_at should be set when locked")
}

func TestUpdateSchema_LockedSchemaIsImmutable(t *testing.T) {
	store := newMemSchemaStore()
	ctx := context.Background()

	// Pre-seed a locked schema.
	require.NoError(t, store.UpsertPayloadSchema(ctx, &PayloadSchema{
		SourceID:    "src-1",
		Fields:      map[string]FieldSchema{"a": {Name: "a", Type: "number"}},
		SampleCount: 5,
		Version:     1,
		IsLocked:    true,
	}))

	UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{
		"a": 1.0,
		"b": "should-not-be-added",
	}, 10)

	got, err := store.GetPayloadSchema(ctx, "src-1")
	require.NoError(t, err)
	assert.NotContains(t, got.Fields, "b", "locked schema should not absorb new fields")
	assert.Equal(t, 5, got.SampleCount, "sample count should not increment on locked schema")
}

func TestUpdateSchema_ExampleCappedAtThree(t *testing.T) {
	store := newMemSchemaStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		UpdateSchema(ctx, store, discardLogger(), "src-1", map[string]any{
			"order_id": "ORD-X",
		}, 100)
	}

	got, err := store.GetPayloadSchema(ctx, "src-1")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got.Fields["order_id"].Examples), 3)
}
