package extraction

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	openaiEndpoint     = "https://api.openai.com/v1/responses"
	defaultOpenAIModel = "gpt-4.1-mini"
)

// OpenAIProvider implements LLMProvider against /v1/responses (the newer endpoint that
// uniformly handles input_image and input_file content blocks — chat completions can't
// take raw PDFs).
type OpenAIProvider struct {
	defaultModel string
}

// NewOpenAIProvider returns a provider with the given default model.
// Pass "" to use the package default ("gpt-4.1-mini").
func NewOpenAIProvider(defaultModel string) *OpenAIProvider {
	if defaultModel == "" {
		defaultModel = defaultOpenAIModel
	}
	return &OpenAIProvider{defaultModel: defaultModel}
}

func (p *OpenAIProvider) Name() string         { return "openai" }
func (p *OpenAIProvider) DefaultModel() string { return p.defaultModel }

func (p *OpenAIProvider) SetAuthHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func (p *OpenAIProvider) BuildRequest(model, fileType string, fileBytes []byte, mediaType string) (string, []byte, error) {
	var content []openaiContentBlock

	switch fileType {
	case "pdf":
		dataURL := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(fileBytes)
		content = append(content, openaiContentBlock{
			Type:     "input_file",
			Filename: "document.pdf",
			FileData: dataURL,
		})
	case "image":
		dataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(fileBytes))
		content = append(content, openaiContentBlock{
			Type:     "input_image",
			ImageURL: dataURL,
			Detail:   "auto",
		})
	default:
		// csv / xml / text (including DOCX-extracted text) — send inline.
		content = append(content, openaiContentBlock{
			Type: "input_text",
			Text: string(fileBytes),
		})
	}

	content = append(content, openaiContentBlock{
		Type: "input_text",
		Text: userExtractionInstruction,
	})

	reqBody := openaiRequest{
		Model:           model,
		Instructions:    extractionSystemPrompt,
		MaxOutputTokens: defaultMaxTokens,
		Input: []openaiInputMessage{
			{Role: "user", Content: content},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("marshal openai request: %w", err)
	}
	return openaiEndpoint, body, nil
}

func (p *OpenAIProvider) ParseResponse(body []byte) (string, error) {
	var parsed openaiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse openai response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openai error %s: %s", parsed.Error.Type, parsed.Error.Message)
	}
	// Responses API: top-level output_text is sometimes provided as a convenience.
	if parsed.OutputText != "" {
		return parsed.OutputText, nil
	}
	// Otherwise walk output[].content[] and concatenate text-typed blocks.
	var out string
	for _, item := range parsed.Output {
		for _, block := range item.Content {
			if block.Type == "output_text" || block.Type == "text" {
				out += block.Text
			}
		}
	}
	return out, nil
}

// OpenAI Responses API request/response shapes — only the fields we use.

type openaiRequest struct {
	Model           string               `json:"model"`
	Instructions    string               `json:"instructions,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Input           []openaiInputMessage `json:"input"`
}

type openaiInputMessage struct {
	Role    string               `json:"role"`
	Content []openaiContentBlock `json:"content"`
}

type openaiContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

type openaiResponse struct {
	OutputText string `json:"output_text,omitempty"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
