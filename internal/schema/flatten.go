package schema

import "fmt"

// FlattenJSON recursively flattens a nested JSON map into dot-notation keys.
// Example: {"a": {"b": 1}} → {"a.b": 1}
func FlattenJSON(data map[string]any) map[string]any {
	result := make(map[string]any)
	flattenRecursive("", data, result)
	return result
}

func flattenRecursive(prefix string, data map[string]any, result map[string]any) {
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]any:
			flattenRecursive(fullKey, v, result)
		case []any:
			result[fullKey] = v
			for i, item := range v {
				itemKey := fmt.Sprintf("%s[%d]", fullKey, i)
				if nested, ok := item.(map[string]any); ok {
					flattenRecursive(itemKey, nested, result)
				} else {
					result[itemKey] = item
				}
			}
		default:
			result[fullKey] = v
		}
	}
}

// DetectType returns the JSON type string for a Go value.
func DetectType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case string:
		return "string"
	case float64, float32, int, int64:
		return "number"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "string"
	}
}
