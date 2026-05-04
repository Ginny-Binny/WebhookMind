package extraction

import "context"

// ExtractRequest is the backend-agnostic extraction request.
// Mirrors pb.ExtractionRequest but decouples downstream code from the protobuf types,
// so alternative backends (cloud LLM, local gRPC, etc.) can share the same call sites.
type ExtractRequest struct {
	EventID      string
	SourceID     string
	FilePath     string // object path in MinIO
	FileType     string // "pdf" | "image" | "audio" | "csv" | "xml"
	PresignedURL string // used by LocalExtractor — the C++ container downloads from this URL
	FileBytes    []byte // optional; set by caller when the file is already in memory. CloudExtractor prefers this over re-downloading.
	APIKey       string // optional BYOK override. CloudExtractor uses this if non-empty, else falls back to its construction-time key.
}

// TranscriptionSegment is a single chunk of transcribed audio.
type TranscriptionSegment struct {
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// ExtractResponse is the backend-agnostic extraction response.
type ExtractResponse struct {
	Success          bool
	ErrorMessage     string
	ExtractedJSON    string
	TemplateID       string
	CacheHit         bool
	DurationMs       int64
	Segments         []TranscriptionSegment
	DetectedLanguage string
}

// Extractor is the backend-agnostic extraction interface.
// Implementations: LocalExtractor (gRPC to the C++ extractor container),
// CloudExtractor (HTTPS to an LLM provider — added in Phase 2).
type Extractor interface {
	Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error)
	Close() error
}
