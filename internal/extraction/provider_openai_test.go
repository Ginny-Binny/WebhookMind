package extraction

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIProvider_DefaultModel(t *testing.T) {
	p := NewOpenAIProvider("")
	assert.Equal(t, "gpt-4.1-mini", p.DefaultModel())

	custom := NewOpenAIProvider("gpt-4o")
	assert.Equal(t, "gpt-4o", custom.DefaultModel())
}

func TestOpenAIProvider_BuildRequest_Image(t *testing.T) {
	p := NewOpenAIProvider("")
	endpoint, body, err := p.BuildRequest("gpt-4.1-mini", "image", []byte("fakeimagebytes"), "image/png")
	require.NoError(t, err)
	assert.Equal(t, openaiEndpoint, endpoint)

	var parsed openaiRequest
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.Equal(t, "gpt-4.1-mini", parsed.Model)
	assert.Equal(t, extractionSystemPrompt, parsed.Instructions)
	require.Len(t, parsed.Input, 1)
	require.GreaterOrEqual(t, len(parsed.Input[0].Content), 2)

	// First block must be an input_image with a data URL.
	imgBlock := parsed.Input[0].Content[0]
	assert.Equal(t, "input_image", imgBlock.Type)
	assert.True(t, strings.HasPrefix(imgBlock.ImageURL, "data:image/png;base64,"), "image url should be a data URL, got: %s", imgBlock.ImageURL)

	// Last block must be the instruction nudge.
	last := parsed.Input[0].Content[len(parsed.Input[0].Content)-1]
	assert.Equal(t, "input_text", last.Type)
	assert.Equal(t, userExtractionInstruction, last.Text)
}

func TestOpenAIProvider_BuildRequest_PDF(t *testing.T) {
	p := NewOpenAIProvider("")
	_, body, err := p.BuildRequest("gpt-4.1-mini", "pdf", []byte("fake-pdf-bytes"), "application/pdf")
	require.NoError(t, err)

	var parsed openaiRequest
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Len(t, parsed.Input, 1)
	pdfBlock := parsed.Input[0].Content[0]
	assert.Equal(t, "input_file", pdfBlock.Type)
	assert.NotEmpty(t, pdfBlock.Filename)
	assert.True(t, strings.HasPrefix(pdfBlock.FileData, "data:application/pdf;base64,"), "file data should be a data URL, got: %s", pdfBlock.FileData)
}

func TestOpenAIProvider_BuildRequest_Text(t *testing.T) {
	p := NewOpenAIProvider("")
	_, body, err := p.BuildRequest("gpt-4.1-mini", "text", []byte("hello world"), "text/plain")
	require.NoError(t, err)

	var parsed openaiRequest
	require.NoError(t, json.Unmarshal(body, &parsed))
	require.Len(t, parsed.Input, 1)
	first := parsed.Input[0].Content[0]
	assert.Equal(t, "input_text", first.Type)
	assert.Equal(t, "hello world", first.Text)
}

func TestOpenAIProvider_ParseResponse_OutputText(t *testing.T) {
	p := NewOpenAIProvider("")
	body := []byte(`{"output_text":"{\"order_id\":\"123\"}"}`)
	out, err := p.ParseResponse(body)
	require.NoError(t, err)
	assert.Equal(t, `{"order_id":"123"}`, out)
}

func TestOpenAIProvider_ParseResponse_NestedOutput(t *testing.T) {
	p := NewOpenAIProvider("")
	body := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"hello "},{"type":"output_text","text":"world"}]}]}`)
	out, err := p.ParseResponse(body)
	require.NoError(t, err)
	assert.Equal(t, "hello world", out)
}

func TestOpenAIProvider_ParseResponse_Error(t *testing.T) {
	p := NewOpenAIProvider("")
	body := []byte(`{"error":{"type":"invalid_request_error","message":"bad model"}}`)
	_, err := p.ParseResponse(body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_request_error")
	assert.Contains(t, err.Error(), "bad model")
}

func TestAnthropicProvider_DefaultModel(t *testing.T) {
	p := NewAnthropicProvider("")
	assert.Equal(t, "claude-haiku-4-5", p.DefaultModel())

	custom := NewAnthropicProvider("claude-opus-4-7")
	assert.Equal(t, "claude-opus-4-7", custom.DefaultModel())
}
