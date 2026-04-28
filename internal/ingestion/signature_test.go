package ingestion

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testSecret = "demo-secret-1234"

var fixedNow = time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

func TestVerify_Success(t *testing.T) {
	body := []byte(`{"order_id":"OK-1"}`)
	header := SignForTest(testSecret, body, fixedNow)

	err := Verify(testSecret, header, body, fixedNow, 5*time.Minute)
	assert.NoError(t, err)
}

func TestVerify_EmptySecret(t *testing.T) {
	err := Verify("", "t=1,v1=abc", []byte(`{}`), fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrInvalidSecret)
}

func TestVerify_MissingHeader(t *testing.T) {
	err := Verify(testSecret, "", []byte(`{}`), fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrMissingHeader)
}

func TestVerify_MalformedHeader(t *testing.T) {
	cases := []string{
		"justgarbage",
		"v1=abc",                      // missing t
		"t=123",                       // missing v1
		"t=notanumber,v1=abcdef",      // non-numeric timestamp
		"t=123,v1=zzz-not-hex-string", // non-hex signature
		"=,=",                         // empty keys/values
	}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			err := Verify(testSecret, h, []byte(`{}`), fixedNow, 5*time.Minute)
			assert.Error(t, err, "expected an error for header %q", h)
			// Any of malformed/missing/mismatch are acceptable failure modes — what
			// matters is we never accept these as valid.
			assert.False(t, errors.Is(err, nil))
		})
	}
}

func TestVerify_StaleTimestamp(t *testing.T) {
	body := []byte(`{"x":1}`)

	// Signed 10 minutes ago — outside the 5-minute window.
	signedAt := fixedNow.Add(-10 * time.Minute)
	header := SignForTest(testSecret, body, signedAt)

	err := Verify(testSecret, header, body, fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrStaleTimestamp)
}

func TestVerify_FutureTimestamp(t *testing.T) {
	body := []byte(`{"x":1}`)

	// Signed 10 minutes in the future — also outside the window.
	signedAt := fixedNow.Add(10 * time.Minute)
	header := SignForTest(testSecret, body, signedAt)

	err := Verify(testSecret, header, body, fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrStaleTimestamp)
}

func TestVerify_TamperedBody(t *testing.T) {
	body := []byte(`{"order_id":"OK-1"}`)
	header := SignForTest(testSecret, body, fixedNow)

	// Body modified after signing — signature should no longer match.
	tampered := []byte(`{"order_id":"NOT-OK"}`)
	err := Verify(testSecret, header, tampered, fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestVerify_WrongSecret(t *testing.T) {
	body := []byte(`{"x":1}`)
	header := SignForTest(testSecret, body, fixedNow)

	err := Verify("a-different-secret", header, body, fixedNow, 5*time.Minute)
	assert.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestVerify_KeyOrderInsensitive(t *testing.T) {
	body := []byte(`{}`)

	// Compute signature normally then swap key order in header.
	normal := SignForTest(testSecret, body, fixedNow)
	// Split, reverse the two parts.
	swapped := ""
	for i, part := range []string{normal[len("t=") : findComma(normal)+0], normal[len(",v1=")+findComma(normal):]} {
		_ = i
		_ = part
	}
	// Easier: build manually.
	// SignForTest output is exactly "t=<ts>,v1=<hex>" — reverse to "v1=<hex>,t=<ts>"
	parts := splitTwo(normal, ",")
	swapped = parts[1] + "," + parts[0]

	err := Verify(testSecret, swapped, body, fixedNow, 5*time.Minute)
	assert.NoError(t, err)
}

func TestVerify_ExtraUnknownKeysIgnored(t *testing.T) {
	body := []byte(`{}`)
	base := SignForTest(testSecret, body, fixedNow)
	// Append a forward-compat v2 key — should be ignored.
	withExtra := base + ",v2=should-be-ignored"

	err := Verify(testSecret, withExtra, body, fixedNow, 5*time.Minute)
	assert.NoError(t, err)
}

func TestVerify_BoundaryTimestampWithinWindow(t *testing.T) {
	body := []byte(`{}`)
	// Just inside the 5-minute window.
	signedAt := fixedNow.Add(-4 * time.Minute)
	header := SignForTest(testSecret, body, signedAt)

	err := Verify(testSecret, header, body, fixedNow, 5*time.Minute)
	assert.NoError(t, err)
}

// --- small string helpers used by the key-order test ---

func findComma(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

func splitTwo(s, sep string) [2]string {
	idx := -1
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return [2]string{s, ""}
	}
	return [2]string{s[:idx], s[idx+len(sep):]}
}
