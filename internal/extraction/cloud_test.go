package extraction

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{200, false},
		{201, false},
		{204, false},
		{301, false},
		{400, false}, // bad request — caller bug
		{401, false}, // bad API key — fail fast
		{403, false}, // permission — fail fast
		{404, false}, // not found — fail fast
		{422, false}, // validation
		{429, true},  // rate limit
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{599, true},
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			assert.Equalf(t, tc.want, isRetryableStatus(tc.code), "status %d", tc.code)
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Run("integer seconds", func(t *testing.T) {
		assert.Equal(t, 5*time.Second, parseRetryAfter("5"))
	})

	t.Run("integer seconds with whitespace", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, parseRetryAfter("  30  "))
	})

	t.Run("zero or negative returns 0 (use default backoff)", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), parseRetryAfter("0"))
		assert.Equal(t, time.Duration(0), parseRetryAfter("-5"))
	})

	t.Run("HTTP-date in the future", func(t *testing.T) {
		// Use http.TimeFormat (the canonical HTTP date format with GMT, not UTC).
		future := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
		got := parseRetryAfter(future)
		// Allow a generous window — the parse loses sub-second precision and clock can drift.
		assert.True(t, got > 90*time.Second && got < 130*time.Second, "expected ~2 minutes, got %v", got)
	})

	t.Run("HTTP-date in the past returns 0", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat)
		assert.Equal(t, time.Duration(0), parseRetryAfter(past))
	})

	t.Run("malformed returns 0", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), parseRetryAfter("not-a-number"))
	})

	t.Run("empty returns 0", func(t *testing.T) {
		assert.Equal(t, time.Duration(0), parseRetryAfter(""))
	})
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		fileType string
		want     string
	}{
		{"pdf", "application/pdf"},
		{"image", "image/png"},
		{"csv", "text/csv"},
		{"xml", "application/xml"},
		{"unknown-stuff", "application/octet-stream"},
		{"", "application/octet-stream"},
	}
	for _, tc := range tests {
		t.Run(tc.fileType, func(t *testing.T) {
			assert.Equal(t, tc.want, inferMediaType(tc.fileType))
		})
	}
}
