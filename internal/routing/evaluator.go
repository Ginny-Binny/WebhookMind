package routing

import (
	"fmt"
	"strconv"
	"strings"
)

// EvaluateCondition checks if a single condition matches the flattened payload.
func EvaluateCondition(cond Condition, flat map[string]any) bool {
	value, exists := flat[cond.Field]

	switch cond.Operator {
	case "exists":
		return exists
	case "not_exists":
		return !exists
	}

	if !exists {
		return false
	}

	switch cond.Operator {
	case "eq":
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", cond.Value)
	case "neq":
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", cond.Value)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", value), fmt.Sprintf("%v", cond.Value))
	case "gt", "gte", "lt", "lte":
		return compareNumeric(value, cond.Value, cond.Operator)
	}

	return false
}

func compareNumeric(a, b any, op string) bool {
	af := toFloat(a)
	bf := toFloat(b)
	if af == nil || bf == nil {
		return false
	}

	switch op {
	case "gt":
		return *af > *bf
	case "gte":
		return *af >= *bf
	case "lt":
		return *af < *bf
	case "lte":
		return *af <= *bf
	}
	return false
}

func toFloat(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case float32:
		f := float64(n)
		return &f
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return nil
		}
		return &f
	}
	return nil
}

// EvaluateConditions evaluates a list of conditions with a logic operator.
func EvaluateConditions(conditions []Condition, flat map[string]any, logic string) bool {
	if len(conditions) == 0 {
		return false
	}

	if logic == "OR" {
		for _, c := range conditions {
			if EvaluateCondition(c, flat) {
				return true
			}
		}
		return false
	}

	// Default: AND
	for _, c := range conditions {
		if !EvaluateCondition(c, flat) {
			return false
		}
	}
	return true
}
