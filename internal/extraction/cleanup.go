package extraction

import "strings"

// StripCodeFences removes markdown code fences that LLMs often wrap JSON in,
// returning the first fenced block's contents or the original string if no fence is present.
// Handles: "```json\n{...}\n```", "```\n{...}\n```", "{...}\n```\n<more text>", and plain JSON.
//
// Used by both the local (llama.cpp) and cloud (Anthropic) extractor backends since
// either one can occasionally emit fenced output despite the system prompt asking otherwise.
func StripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Case 1: already starts with JSON. Trim at the first trailing fence if present
	// (some LLMs emit valid JSON then more code blocks — we want only the first).
	if s[0] == '{' || s[0] == '[' {
		if idx := strings.Index(s, "```"); idx >= 0 {
			return strings.TrimSpace(s[:idx])
		}
		return s
	}

	// Case 2: starts with a fence. Skip past the opening ``` and optional language tag.
	start := strings.Index(s, "```")
	if start < 0 {
		return s
	}
	rest := s[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
