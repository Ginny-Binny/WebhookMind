package extraction

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LLMProvider abstracts a cloud LLM extraction provider (Anthropic, OpenAI, ...).
// CloudExtractor owns the retry loop, file download, audio rejection, and DOCX
// conversion — provider implementations only know how to build a request for their
// own API, set the right auth headers, and parse the response.
type LLMProvider interface {
	// Name returns a stable identifier used in error messages and routing ("anthropic", "openai").
	Name() string

	// DefaultModel is used when no per-request or per-deployment model is configured.
	DefaultModel() string

	// BuildRequest constructs the provider-specific JSON body for an extraction call.
	// fileType is one of: "pdf", "image", "csv", "xml", "text" (DOCX is converted to text upstream).
	// Returns the absolute endpoint URL and the JSON-encoded request body.
	BuildRequest(model, fileType string, fileBytes []byte, mediaType string) (endpoint string, body []byte, err error)

	// SetAuthHeaders applies provider-specific auth headers to an outgoing HTTP request.
	// (Anthropic uses x-api-key + anthropic-version, OpenAI uses Authorization: Bearer.)
	SetAuthHeaders(req *http.Request, apiKey string)

	// ParseResponse extracts the model-generated text from a successful HTTP response body.
	ParseResponse(body []byte) (string, error)
}

// callClassification describes whether a failed provider call should be retried,
// and how long to wait if so. Shared by all providers.
type callClassification struct {
	retryable  bool
	retryAfter time.Duration // from Retry-After header when available; 0 means "use backoff default"
}

// extractionSystemPrompt is the instruction sent to every cloud provider. Each provider
// puts it in a slightly different place (Anthropic: `system` block, OpenAI: `instructions`).
const extractionSystemPrompt = `You are a document extraction engine. Given a document, identify every structured field present and return them as a single flat JSON object.

Rules:
- Field names MUST be lowercase snake_case (e.g. "invoice_number", "total_amount").
- Return ONLY the JSON object. No explanations, no markdown code fences, no prose before or after.
- Use correct JSON types: numbers for numeric values, booleans for true/false, strings otherwise.
- For dates, prefer ISO 8601 format when the original format is unambiguous.
- If the document contains no extractable structured data, return an empty object {}.`

// userExtractionInstruction is the per-message nudge attached after the document content.
const userExtractionInstruction = "Extract the structured fields from this document."

// defaultMaxTokens caps the model's response length. Both providers honor this (under
// different field names) — providers reference this constant when building their request.
const defaultMaxTokens = 4096

// isRetryableStatus returns true for status codes that represent transient server-side problems.
// 400/401/403/404 are caller bugs or missing auth — retrying won't help and masks the real issue.
func isRetryableStatus(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}
	return code >= 500 && code < 600
}

// parseRetryAfter accepts either a delta-seconds integer or an HTTP-date.
// Returns 0 if the header is missing or unparseable, signaling "use default backoff".
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
