package extraction

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	anthropicEndpoint     = "https://api.anthropic.com/v1/messages"
	anthropicVersion      = "2023-06-01"
	defaultAnthropicModel = "claude-haiku-4-5"
)

// AnthropicProvider implements LLMProvider against /v1/messages.
type AnthropicProvider struct {
	defaultModel string
}

// NewAnthropicProvider returns a provider with the given default model.
// Pass "" to use the package default ("claude-haiku-4-5").
func NewAnthropicProvider(defaultModel string) *AnthropicProvider {
	if defaultModel == "" {
		defaultModel = defaultAnthropicModel
	}
	return &AnthropicProvider{defaultModel: defaultModel}
}

func (p *AnthropicProvider) Name() string         { return "anthropic" }
func (p *AnthropicProvider) DefaultModel() string { return p.defaultModel }

func (p *AnthropicProvider) SetAuthHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

func (p *AnthropicProvider) BuildRequest(model, fileType string, fileBytes []byte, mediaType string) (string, []byte, error) {
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
		// csv / xml / text (including DOCX-extracted text) — send inline.
		content = append(content, anthropicContentBlock{
			Type: "text",
			Text: string(fileBytes),
		})
	}

	content = append(content, anthropicContentBlock{
		Type: "text",
		Text: userExtractionInstruction,
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

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	return anthropicEndpoint, body, nil
}

func (p *AnthropicProvider) ParseResponse(body []byte) (string, error) {
	var parsed anthropicResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse anthropic response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("anthropic error %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	var out string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			out += block.Text
		}
	}
	return out, nil
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
