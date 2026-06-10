package extraction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// CloudExtractorOptions configures the multi-provider cloud extractor.
//
// Empty AnthropicAPIKey + empty OpenAIAPIKey is allowed — that's the BYOK ("bring your
// own key") deployment mode where the server has no LLM credit budget and every webhook
// must carry an X-Anthropic-Key or X-OpenAI-Key header. In that mode, Extract() returns
// a clear error if neither the per-request key nor a matching server-side key is set.
// This is what makes the public demo deployment safe to host: you can't be billed for
// traffic that doesn't include a recruiter's own key.
type CloudExtractorOptions struct {
	AnthropicAPIKey string
	AnthropicModel  string // empty = use provider default
	OpenAIAPIKey    string
	OpenAIModel     string // empty = use provider default
	DefaultProvider string // "anthropic" or "openai" — used when ExtractRequest.Provider is empty
	TimeoutSeconds  int
	Logger          *slog.Logger
}

// CloudExtractor implements the Extractor interface by dispatching to one of several
// LLMProvider implementations (Anthropic, OpenAI). The retry/backoff loop, file download,
// audio rejection, and DOCX-to-text conversion are owned here; provider implementations
// only know how to build their own API request shape and parse their own response.
//
// Audio is not supported by either cloud provider in Phase 2 — callers should fall back
// to the local backend for audio.
type CloudExtractor struct {
	providers       map[string]LLMProvider
	apiKeys         map[string]string // server-side keys, keyed by provider name
	defaultProvider string
	httpClient      *http.Client
	logger          *slog.Logger
}

// NewCloudExtractor builds a CloudExtractor with both Anthropic and OpenAI providers wired up.
func NewCloudExtractor(opts CloudExtractorOptions) (*CloudExtractor, error) {
	if opts.TimeoutSeconds <= 0 {
		opts.TimeoutSeconds = 60
	}
	if opts.DefaultProvider == "" {
		opts.DefaultProvider = "anthropic"
	}
	if opts.DefaultProvider != "anthropic" && opts.DefaultProvider != "openai" {
		return nil, fmt.Errorf("invalid default provider %q (expected 'anthropic' or 'openai')", opts.DefaultProvider)
	}

	providers := map[string]LLMProvider{
		"anthropic": NewAnthropicProvider(opts.AnthropicModel),
		"openai":    NewOpenAIProvider(opts.OpenAIModel),
	}
	apiKeys := map[string]string{
		"anthropic": opts.AnthropicAPIKey,
		"openai":    opts.OpenAIAPIKey,
	}

	if opts.AnthropicAPIKey == "" && opts.OpenAIAPIKey == "" && opts.Logger != nil {
		opts.Logger.Warn("cloud extractor: no server-side LLM keys configured — running in BYOK mode (every request must carry X-Anthropic-Key or X-OpenAI-Key)")
	}

	return &CloudExtractor{
		providers:       providers,
		apiKeys:         apiKeys,
		defaultProvider: opts.DefaultProvider,
		httpClient: &http.Client{
			Timeout: time.Duration(opts.TimeoutSeconds) * time.Second,
		},
		logger: opts.Logger,
	}, nil
}

func (c *CloudExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	startTime := time.Now()
	elapsed := func() int64 { return time.Since(startTime).Milliseconds() }

	// Audio path is unsupported by both cloud providers; return a clear failure so the
	// caller can fall back to the local backend or skip extraction.
	if req.FileType == "audio" {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: "audio extraction not supported by cloud backend",
			DurationMs:   elapsed(),
		}, nil
	}

	// Resolve which provider to use for THIS request.
	providerName := req.Provider
	if providerName == "" {
		providerName = c.defaultProvider
	}
	provider, ok := c.providers[providerName]
	if !ok {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("unknown provider %q (supported: anthropic, openai)", providerName),
			DurationMs:   elapsed(),
		}, nil
	}

	// Resolve which API key to use. Per-request override (BYOK) wins.
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = c.apiKeys[providerName]
	}
	if apiKey == "" {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("no %s API key — provide one via X-%s-Key header or set the server-side key", providerName, providerHeaderName(providerName)),
			DurationMs:   elapsed(),
		}, nil
	}

	// Resolve which model to use. Per-request override wins.
	model := req.Model
	if model == "" {
		model = provider.DefaultModel()
	}

	// Prefer bytes passed in by the caller (extractor-bridge already downloaded the file).
	// Fall back to downloading from the presigned URL only if no bytes were provided.
	fileBytes := req.FileBytes
	mediaType := inferMediaType(req.FileType)
	if len(fileBytes) == 0 {
		var err error
		fileBytes, mediaType, err = c.downloadFile(ctx, req.PresignedURL, req.FileType)
		if err != nil {
			return &ExtractResponse{
				Success:      false,
				ErrorMessage: fmt.Sprintf("file download: %v", err),
				DurationMs:   elapsed(),
			}, nil
		}
	}

	// DOCX → text. Neither provider accepts raw .docx as input. We unzip it here and
	// hand the LLM plain text; structured extraction still happens at the LLM.
	fileType := req.FileType
	if fileType == "docx" {
		text, err := ExtractDocxText(fileBytes)
		if err != nil {
			return &ExtractResponse{
				Success:      false,
				ErrorMessage: fmt.Sprintf("docx extraction: %v", err),
				DurationMs:   elapsed(),
			}, nil
		}
		fileBytes = []byte(text)
		fileType = "text"
		mediaType = "text/plain"
	}

	endpoint, body, err := provider.BuildRequest(model, fileType, fileBytes, mediaType)
	if err != nil {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("build request: %v", err),
			DurationMs:   elapsed(),
		}, nil
	}

	extractedText, err := c.callProvider(ctx, provider, endpoint, apiKey, body)
	if err != nil {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: err.Error(),
			DurationMs:   elapsed(),
		}, nil
	}

	return &ExtractResponse{
		Success:       true,
		ExtractedJSON: extractedText,
		DurationMs:    elapsed(),
	}, nil
}

func (c *CloudExtractor) Close() error { return nil }

func (c *CloudExtractor) downloadFile(ctx context.Context, url, fileType string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = inferMediaType(fileType)
	}
	return data, mediaType, nil
}

func inferMediaType(fileType string) string {
	switch fileType {
	case "pdf":
		return "application/pdf"
	case "image":
		return "image/png"
	case "csv":
		return "text/csv"
	case "xml":
		return "application/xml"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	return "application/octet-stream"
}

// providerHeaderName maps a provider name to the canonical capitalization used in
// the BYOK request header (X-Anthropic-Key, X-OpenAI-Key). Only used for error messages.
func providerHeaderName(provider string) string {
	switch provider {
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	}
	return provider
}

// callProvider wraps a single-shot provider call with bounded exponential backoff on
// transient failures. Retryable: network errors, 429 (honoring Retry-After), 5xx.
// Non-retryable: 400, 401, 403, 404 — these fail fast so operators see the real issue.
func (c *CloudExtractor) callProvider(ctx context.Context, provider LLMProvider, endpoint, apiKey string, body []byte) (string, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, classification, err := c.callProviderOnce(ctx, provider, endpoint, apiKey, body)
		if err == nil {
			if attempt > 1 && c.logger != nil {
				c.logger.Info("provider call succeeded after retry",
					"provider", provider.Name(),
					"attempt", attempt,
				)
			}
			return result, nil
		}

		lastErr = err
		if !classification.retryable || attempt == maxAttempts {
			return "", err
		}

		delay := classification.retryAfter
		if delay <= 0 {
			delay = time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s, 4s
		}

		if c.logger != nil {
			c.logger.Warn("provider call failed, retrying",
				"provider", provider.Name(),
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"wait", delay.String(),
				"error", err.Error(),
			)
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	return "", lastErr
}

func (c *CloudExtractor) callProviderOnce(ctx context.Context, provider LLMProvider, endpoint, apiKey string, body []byte) (string, callClassification, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", callClassification{retryable: false}, fmt.Errorf("build http request: %w", err)
	}
	provider.SetAuthHeaders(httpReq, apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Transport errors (DNS, connection refused, timeout) are generally retryable
		// unless the context was cancelled.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", callClassification{retryable: false}, fmt.Errorf("%s api call: %w", provider.Name(), err)
		}
		return "", callClassification{retryable: true}, fmt.Errorf("%s api call: %w", provider.Name(), err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", callClassification{retryable: true}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		class := callClassification{retryable: isRetryableStatus(resp.StatusCode)}
		if resp.StatusCode == http.StatusTooManyRequests {
			class.retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
		return "", class, fmt.Errorf("%s api returned %d: %s", provider.Name(), resp.StatusCode, truncate(string(respBody), 500))
	}

	text, err := provider.ParseResponse(respBody)
	if err != nil {
		return "", callClassification{retryable: false}, err
	}
	return text, callClassification{}, nil
}
