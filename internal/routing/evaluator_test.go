package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateCondition(t *testing.T) {
	flat := map[string]any{
		"amount":   500.0,
		"currency": "USD",
		"customer": "Acme Corp",
		"priority": "high",
		"count":    int64(42),
	}

	tests := []struct {
		name string
		cond Condition
		want bool
	}{
		// eq
		{"eq matches string", Condition{Field: "currency", Operator: "eq", Value: "USD"}, true},
		{"eq matches number-as-string", Condition{Field: "amount", Operator: "eq", Value: "500"}, true},
		{"eq mismatch", Condition{Field: "currency", Operator: "eq", Value: "EUR"}, false},
		{"eq missing field", Condition{Field: "missing", Operator: "eq", Value: "x"}, false},

		// neq
		{"neq matches", Condition{Field: "currency", Operator: "neq", Value: "EUR"}, true},
		{"neq mismatch", Condition{Field: "currency", Operator: "neq", Value: "USD"}, false},
		{"neq missing field", Condition{Field: "missing", Operator: "neq", Value: "x"}, false},

		// contains
		{"contains substring", Condition{Field: "customer", Operator: "contains", Value: "Acme"}, true},
		{"contains misses", Condition{Field: "customer", Operator: "contains", Value: "Globex"}, false},

		// gt / gte
		{"gt true", Condition{Field: "amount", Operator: "gt", Value: 100}, true},
		{"gt false equal", Condition{Field: "amount", Operator: "gt", Value: 500}, false},
		{"gte equal", Condition{Field: "amount", Operator: "gte", Value: 500}, true},
		{"gte greater", Condition{Field: "amount", Operator: "gte", Value: 100}, true},
		{"gte less", Condition{Field: "amount", Operator: "gte", Value: 600}, false},

		// lt / lte
		{"lt true", Condition{Field: "amount", Operator: "lt", Value: 1000}, true},
		{"lt false equal", Condition{Field: "amount", Operator: "lt", Value: 500}, false},
		{"lte equal", Condition{Field: "amount", Operator: "lte", Value: 500}, true},
		{"lte less", Condition{Field: "amount", Operator: "lte", Value: 1000}, true},
		{"lte greater", Condition{Field: "amount", Operator: "lte", Value: 100}, false},

		// gt with int64 (numeric coercion)
		{"gt with int64", Condition{Field: "count", Operator: "gt", Value: 40}, true},

		// gt with non-numeric value falls back to false
		{"gt on string", Condition{Field: "currency", Operator: "gt", Value: "AAA"}, false},

		// exists / not_exists — these check presence, not value
		{"exists present", Condition{Field: "amount", Operator: "exists"}, true},
		{"exists absent", Condition{Field: "missing", Operator: "exists"}, false},
		{"not_exists present", Condition{Field: "amount", Operator: "not_exists"}, false},
		{"not_exists absent", Condition{Field: "missing", Operator: "not_exists"}, true},

		// unknown operator
		{"unknown operator", Condition{Field: "amount", Operator: "regex", Value: ".*"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateCondition(tc.cond, flat)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEvaluateConditions(t *testing.T) {
	flat := map[string]any{
		"amount":   500.0,
		"currency": "USD",
	}

	condUSD := Condition{Field: "currency", Operator: "eq", Value: "USD"}
	condEUR := Condition{Field: "currency", Operator: "eq", Value: "EUR"}
	condBig := Condition{Field: "amount", Operator: "gt", Value: 100}
	condTiny := Condition{Field: "amount", Operator: "lt", Value: 10}

	t.Run("AND all match", func(t *testing.T) {
		assert.True(t, EvaluateConditions([]Condition{condUSD, condBig}, flat, "AND"))
	})

	t.Run("AND one misses", func(t *testing.T) {
		assert.False(t, EvaluateConditions([]Condition{condUSD, condTiny}, flat, "AND"))
	})

	t.Run("OR one matches", func(t *testing.T) {
		assert.True(t, EvaluateConditions([]Condition{condEUR, condBig}, flat, "OR"))
	})

	t.Run("OR none match", func(t *testing.T) {
		assert.False(t, EvaluateConditions([]Condition{condEUR, condTiny}, flat, "OR"))
	})

	t.Run("empty conditions returns false", func(t *testing.T) {
		assert.False(t, EvaluateConditions(nil, flat, "AND"))
		assert.False(t, EvaluateConditions([]Condition{}, flat, "OR"))
	})

	t.Run("default logic is AND", func(t *testing.T) {
		// Empty logic operator should behave as AND — both must match.
		assert.True(t, EvaluateConditions([]Condition{condUSD, condBig}, flat, ""))
		assert.False(t, EvaluateConditions([]Condition{condUSD, condTiny}, flat, ""))
	})
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want *float64
	}{
		{"float64", 1.5, ptr(1.5)},
		{"float32", float32(2.5), ptr(2.5)},
		{"int", 42, ptr(42.0)},
		{"int64", int64(99), ptr(99.0)},
		{"string number", "3.14", ptr(3.14)},
		{"string non-numeric", "hello", nil},
		{"bool", true, nil},
		{"nil", nil, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toFloat(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			if assert.NotNil(t, got) {
				assert.InDelta(t, *tc.want, *got, 0.0001)
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }
