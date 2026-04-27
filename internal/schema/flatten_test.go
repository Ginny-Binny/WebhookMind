package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlattenJSON(t *testing.T) {
	t.Run("flat object unchanged", func(t *testing.T) {
		in := map[string]any{"a": 1, "b": "two"}
		got := FlattenJSON(in)
		assert.Equal(t, map[string]any{"a": 1, "b": "two"}, got)
	})

	t.Run("nested object dot-flattened", func(t *testing.T) {
		in := map[string]any{
			"order": map[string]any{
				"id":     "ORD-1",
				"amount": 500.0,
			},
		}
		got := FlattenJSON(in)
		assert.Equal(t, "ORD-1", got["order.id"])
		assert.InDelta(t, 500.0, got["order.amount"], 0.001)
	})

	t.Run("deeply nested", func(t *testing.T) {
		in := map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c": "deep",
				},
			},
		}
		got := FlattenJSON(in)
		assert.Equal(t, "deep", got["a.b.c"])
	})

	t.Run("array of primitives indexed", func(t *testing.T) {
		in := map[string]any{
			"tags": []any{"red", "blue"},
		}
		got := FlattenJSON(in)
		// The full array is preserved at the parent key…
		assert.Contains(t, got, "tags")
		// …and indexed entries are also produced.
		assert.Equal(t, "red", got["tags[0]"])
		assert.Equal(t, "blue", got["tags[1]"])
	})

	t.Run("array of objects flattened with bracket index", func(t *testing.T) {
		in := map[string]any{
			"items": []any{
				map[string]any{"sku": "ARC-01", "qty": 2.0},
				map[string]any{"sku": "MK-47", "qty": 1.0},
			},
		}
		got := FlattenJSON(in)
		assert.Equal(t, "ARC-01", got["items[0].sku"])
		assert.InDelta(t, 2.0, got["items[0].qty"], 0.001)
		assert.Equal(t, "MK-47", got["items[1].sku"])
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		got := FlattenJSON(map[string]any{})
		assert.Empty(t, got)
	})
}

func TestDetectType(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "null"},
		{"string", "hello", "string"},
		{"float64", 1.5, "number"},
		{"float32", float32(1.5), "number"},
		{"int", 42, "number"},
		{"int64", int64(42), "number"},
		{"bool", true, "boolean"},
		{"object", map[string]any{"k": 1}, "object"},
		{"array", []any{1, 2}, "array"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, DetectType(tc.v))
		})
	}
}
