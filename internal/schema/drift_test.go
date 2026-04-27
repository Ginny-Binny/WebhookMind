package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memDriftStore extends memSchemaStore with drift event recording, satisfying DriftStore.
type memDriftStore struct {
	*memSchemaStore
	drifts []DriftEvent
}

func newMemDriftStore() *memDriftStore {
	return &memDriftStore{memSchemaStore: newMemSchemaStore()}
}

func (m *memDriftStore) InsertDriftEvent(_ context.Context, e *DriftEvent) error {
	m.drifts = append(m.drifts, *e)
	return nil
}

func TestCheckDrift_NoSchemaReturnsNil(t *testing.T) {
	store := newMemDriftStore()
	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{"a": 1.0})
	assert.Nil(t, got)
}

func TestCheckDrift_UnlockedSchemaReturnsNil(t *testing.T) {
	store := newMemDriftStore()
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields:   map[string]FieldSchema{"a": {Name: "a", Type: "number"}},
		IsLocked: false,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{"a": 1.0, "new_field": "x"})

	// Schema isn't locked, so drift detection is intentionally skipped.
	assert.Nil(t, got)
	assert.Empty(t, store.drifts)
}

func TestCheckDrift_NewField(t *testing.T) {
	store := newMemDriftStore()
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields:   map[string]FieldSchema{"order_id": {Name: "order_id", Type: "string"}},
		IsLocked: true,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{
		"order_id": "ORD-1",
		"priority": "high", // never seen before
	})

	require.Len(t, got, 1)
	assert.Equal(t, "NEW_FIELD", got[0].DriftType)
	assert.Equal(t, "priority", got[0].FieldName)
	assert.Equal(t, "string", got[0].ActualType)

	// Persisted to store.
	require.Len(t, store.drifts, 1)
	assert.Equal(t, "NEW_FIELD", store.drifts[0].DriftType)
}

func TestCheckDrift_TypeChanged(t *testing.T) {
	store := newMemDriftStore()
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields:   map[string]FieldSchema{"amount": {Name: "amount", Type: "number"}},
		IsLocked: true,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{
		"amount": "two hundred", // schema expected number
	})

	require.Len(t, got, 1)
	assert.Equal(t, "TYPE_CHANGED", got[0].DriftType)
	assert.Equal(t, "amount", got[0].FieldName)
	assert.Equal(t, "number", got[0].ExpectedType)
	assert.Equal(t, "string", got[0].ActualType)
}

func TestCheckDrift_FieldMissing(t *testing.T) {
	store := newMemDriftStore()
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields: map[string]FieldSchema{
			"order_id": {Name: "order_id", Type: "string", Nullable: false},
			"amount":   {Name: "amount", Type: "number", Nullable: false},
		},
		IsLocked: true,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{
		"amount": 500.0,
		// order_id missing
	})

	require.Len(t, got, 1)
	assert.Equal(t, "FIELD_MISSING", got[0].DriftType)
	assert.Equal(t, "order_id", got[0].FieldName)
}

func TestCheckDrift_NullableMissingFieldIsNotDrift(t *testing.T) {
	store := newMemDriftStore()
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields: map[string]FieldSchema{
			"optional_note": {Name: "optional_note", Type: "string", Nullable: true},
		},
		IsLocked: true,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{})
	assert.Empty(t, got, "missing nullable field should not produce drift")
}

func TestCheckDrift_PersistFailureDoesNotMaskReturn(t *testing.T) {
	store := &errInsertStore{newMemDriftStore()}
	require.NoError(t, store.UpsertPayloadSchema(context.Background(), &PayloadSchema{
		SourceID: "src-1",
		Fields:   map[string]FieldSchema{"a": {Name: "a", Type: "number"}},
		IsLocked: true,
	}))

	got := CheckDrift(context.Background(), store, discardLogger(), "src-1", "evt-1", map[string]any{
		"a": 1.0,
		"b": "new",
	})

	// CheckDrift should still return the detected drift to the caller even when
	// the store can't persist it — so e.pub.Publish in the delivery engine still fires.
	require.Len(t, got, 1)
	assert.Equal(t, "NEW_FIELD", got[0].DriftType)
}

type errInsertStore struct {
	*memDriftStore
}

func (e *errInsertStore) InsertDriftEvent(_ context.Context, _ *DriftEvent) error {
	return errors.New("simulated db down")
}
