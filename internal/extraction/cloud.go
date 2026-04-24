package extraction

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	anthropicVersion  = "2023-06-01"
	defaultCloudModel = "claude-haiku-4-5"
	defaultMaxTokens  = 4096
)

// CloudExtractor implements the Extractor interface by calling Anthropic's /v1/messages API.
// Audio is not supported in Phase 2 — callers should fall back to the local backend for audio.
type CloudExtractor struct {
	apiKey     string
	model      string
	httpClient *http.Client
	logger     *slog.Logger
}

func NewCloudExtractor(apiKey, model string, timeoutSeconds int, logger *slog.Logger) (*CloudExtractor, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for cloud extractor backend")
	}
	if model == "" {
		model = defaultCloudModel
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	return &CloudExtractor{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
		logger: logger,
	}, nil
}

func (c *CloudExtractor) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	startTime := time.Now()
	elapsed := func() int64 { return time.Since(startTime).Milliseconds() }

	// Audio path is unsupported by Claude's content API; return a clear failure so the
	// caller can fall back to the local backend or skip extraction.
	if req.FileType == "audio" {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: "audio extraction not supported by cloud backend",
			DurationMs:   elapsed(),
		}, nil
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

	body, err := buildAnthropicRequest(c.model, req.FileType, fileBytes, mediaType)
	if err != nil {
		return &ExtractResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("build request: %v", err),
			DurationMs:   elapsed(),
		}, nil
	}

	extractedText, err := c.callAnthropic(ctx, body)
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
	}
	return "application/octet-stream"
}

// Anthropic API request/response shapes — only the fields we use.

type anthropicRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    []anthropicSystemBlock `json:"system"`
	Messages  []anthropicMessage     `json:"messages"`
}

type anthropicSystemBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	CacheControl *anthropicCache `json:"cache_control,omitempty"`
}

type anthropicCache struct {
	Type string `json:"type"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

const extractionSystemPrompt = `You are a document extraction engine. Given a document, identify every structured field present and return them as a single flat JSON object.

Rules:
- Field names MUST be lowercase snake_case (e.g. "invoice_number", "total_amount").
- Return ONLY the JSON object. No explanations, no markdown code fences, no prose before or after.
- Use correct JSON types: numbers for numeric values, booleans for true/false, strings otherwise.
- For dates, prefer ISO 8601 format when the original format is unambiguous.
- If the document contains no extractable structured data, return an empty object {}.`

func buildAnthropicRequest(model, fileType string, fileBytes []byte, mediaType string) ([]byte, error) {
	var content []anthropicContentBlock

	switch fileType {
	case "pdf":
		content = append(content, anthropicContentBlock{
			Type: "document",
			Source: &anthropicSource{
				Type:      "base64",
				MediaType: "application/pdf",
				Data:      base64.StdEncoding.EncodeToString(fileBytes),
			},
		})
	case "image":
		content = append(content, anthropicContentBlock{
			Type: "image",
			Source: &anthropicSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64.StdEncoding.EncodeToString(fileBytes),
			},
		})
	default:
		// csv / xml / plain text — send inline.
		content = append(content, anthropicContentBlock{
			Type: "text",
			Text: string(fileBytes),
		})
	}

	content = append(content, anthropicContentBlock{
		Type: "text",
		Text: "Extract the structured fields from this document.",
	})

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: defaultMaxTokens,
		System: []anthropicSystemBlock{
			{
				Type:         "text",
				Text:         extractionSystemPrompt,
				CacheControl: &anthropicCache{Type: "ephemeral"},
			},
		},
		Messages: []anthropicMessage{
			{Role: "user", Content: content},
		},
	}

	return json.Marshal(reqBody)
}

func (c *CloudExtractor) callAnthropic(ctx context.Context, body []byte) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("anthropic api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic api returned %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic error %s: %s", parsed.Error.Type, parsed.Error.Message)
	}

	var buf strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			buf.WriteString(block.Text)
		}
	}
	return buf.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
