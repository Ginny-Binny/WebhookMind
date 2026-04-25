package extraction

import (
	"context"
	"log/slog"
)

// FallbackExtractor tries a primary extractor first and, on failure, transparently
// tries a fallback. Useful for cloud-primary-with-local-fallback setups where you
// want cloud latency/quality normally but don't want the pipeline to halt if the
// cloud call fails (auth, outage, rate limit after retries).
type FallbackExtractor struct {
	primary  Extractor
	fallback Extractor
	logger   *slog.Logger
}

func NewFallbackExtractor(primary, fallback Extractor, logger *slog.Logger) *FallbackExtractor {
	return &FallbackExtractor{primary: primary, fallback: fallback, logger: logger}
}

func (f *FallbackExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	resp, err := f.primary.Extract(ctx, req)

	// Happy path — primary succeeded.
	if err == nil && resp != nil && resp.Success {
		return resp, nil
	}

	// Primary failed. Capture the reason for the log, then try fallback.
	reason := "unknown"
	switch {
	case err != nil:
		reason = err.Error()
	case resp != nil && resp.ErrorMessage != "":
		reason = resp.ErrorMessage
	}
	f.logger.Warn("primary extractor failed, trying fallback",
		"event_id", req.EventID,
		"source_id", req.SourceID,
		"file_type", req.FileType,
		"reason", reason,
	)

	fallbackResp, fallbackErr := f.fallback.Extract(ctx, req)
	if fallbackErr == nil && fallbackResp != nil && fallbackResp.Success {
		f.logger.Info("fallback extractor succeeded",
			"event_id", req.EventID,
			"source_id", req.SourceID,
		)
		return fallbackResp, nil
	}

	// Both failed — bubble up the fallback's result so the caller sees the freshest error.
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	return fallbackResp, nil
}

func (f *FallbackExtractor) Close() error {
	// Close both; return the first error encountered (the other is best-effort).
	primaryErr := f.primary.Close()
	fallbackErr := f.fallback.Close()
	if primaryErr != nil {
		return primaryErr
	}
	return fallbackErr
}
