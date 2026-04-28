package ingestion

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors returned by Verify so callers can log the specific failure mode.
var (
	ErrInvalidSecret     = errors.New("signing secret is empty")
	ErrMissingHeader     = errors.New("X-Signature header is missing")
	ErrMalformedHeader   = errors.New("X-Signature header is malformed")
	ErrStaleTimestamp    = errors.New("signature timestamp is outside the allowed window")
	ErrSignatureMismatch = errors.New("signature does not match")
)

// Verify validates a Stripe-style HMAC-SHA256 webhook signature.
//
// Header format:  "t=<unix_seconds>,v1=<lowercase_hex>"
// Signed string:  "<t>.<body>"
// Algorithm:      HMAC-SHA256(secret, signedString)
// Comparison:     constant-time
//
// The (t,v1) ordering is not enforced — pairs may appear in any order, and
// unknown keys are ignored (forward-compatible with future v2 schemes).
func Verify(secret, header string, body []byte, now time.Time, maxAge time.Duration) error {
	if secret == "" {
		return ErrInvalidSecret
	}
	if header == "" {
		return ErrMissingHeader
	}

	tsStr, sigHex, err := parseSignatureHeader(header)
	if err != nil {
		return err
	}

	tsUnix, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ErrMalformedHeader
	}
	signedAt := time.Unix(tsUnix, 0)

	if abs(now.Sub(signedAt)) > maxAge {
		return ErrStaleTimestamp
	}

	expected := computeSignature(secret, tsStr, body)

	provided, err := hex.DecodeString(sigHex)
	if err != nil {
		return ErrMalformedHeader
	}

	if !hmac.Equal(expected, provided) {
		return ErrSignatureMismatch
	}
	return nil
}

// computeSignature returns HMAC-SHA256(secret, "<ts>.<body>") as raw bytes.
func computeSignature(secret, ts string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return mac.Sum(nil)
}

// parseSignatureHeader extracts t and v1 from "t=...,v1=..." (any order, extra keys ignored).
func parseSignatureHeader(h string) (ts, sig string, err error) {
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		eq := strings.IndexByte(part, '=')
		if eq <= 0 {
			continue
		}
		key, value := part[:eq], part[eq+1:]
		switch key {
		case "t":
			ts = value
		case "v1":
			sig = value
		}
	}
	if ts == "" || sig == "" {
		return "", "", ErrMalformedHeader
	}
	return ts, sig, nil
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// SignForTest is a small helper exposed to tests (and to README example tooling) so
// callers don't have to reimplement the signing routine. Returns the X-Signature header value.
func SignForTest(secret string, body []byte, ts time.Time) string {
	tsStr := strconv.FormatInt(ts.Unix(), 10)
	mac := computeSignature(secret, tsStr, body)
	return "t=" + tsStr + ",v1=" + hex.EncodeToString(mac)
}
