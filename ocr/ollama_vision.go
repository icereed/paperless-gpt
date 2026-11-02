package ocr

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	ollamaapi "github.com/ollama/ollama/api"
	"github.com/tmc/langchaingo/llms"
)

// ollamaVisionModel uses Ollama's official client for models that cannot rely
// on a terminal streaming response. In particular, GLM-OCR may end a stream
// without a final done=true chunk, which makes langchaingo discard the text it
// accumulated. The explicit false pointer is important: Ollama defaults to
// streaming when the field is omitted.
type ollamaVisionModel struct {
	client        *ollamaapi.Client
	model         string
	contextLength int
}

var _ llms.Model = (*ollamaVisionModel)(nil)

func newOllamaVisionModel(host, model string, contextLength int, client *http.Client) (*ollamaVisionModel, error) {
	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse Ollama host: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid Ollama host %q", host)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &ollamaVisionModel{
		client:        ollamaapi.NewClient(baseURL, client),
		model:         model,
		contextLength: contextLength,
	}, nil
}

func (m *ollamaVisionModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}

func (m *ollamaVisionModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOptions := llms.CallOptions{
		MaxTokens:   math.MinInt,
		Temperature: math.NaN(),
		TopK:        math.MinInt,
	}
	for _, option := range options {
		option(&callOptions)
	}

	chatMessages, err := ollamaVisionMessages(messages)
	if err != nil {
		return nil, err
	}

	model := m.model
	if callOptions.Model != "" {
		model = callOptions.Model
	}
	requestOptions := make(map[string]any)
	if m.contextLength > 0 {
		requestOptions["num_ctx"] = m.contextLength
	}
	if callOptions.MaxTokens != math.MinInt {
		requestOptions["num_predict"] = callOptions.MaxTokens
	}
	if !math.IsNaN(callOptions.Temperature) {
		requestOptions["temperature"] = callOptions.Temperature
	}
	if callOptions.TopK != math.MinInt {
		requestOptions["top_k"] = callOptions.TopK
	}

	stream := false
	request := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: chatMessages,
		Options:  requestOptions,
		Stream:   &stream,
	}

	var response ollamaapi.ChatResponse
	var content strings.Builder
	received := false
	err = m.client.Chat(ctx, request, func(chunk ollamaapi.ChatResponse) error {
		received = true
		response = chunk
		content.WriteString(chunk.Message.Content)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("Ollama chat request: %w", err)
	}
	if !received {
		return nil, fmt.Errorf("Ollama chat response contained no messages")
	}

	if callOptions.StreamingFunc != nil {
		if err := callOptions.StreamingFunc(ctx, []byte(content.String())); err != nil {
			return nil, err
		}
	}
	if callOptions.StreamingReasoningFunc != nil {
		if err := callOptions.StreamingReasoningFunc(ctx, []byte(response.Message.Thinking), []byte(content.String())); err != nil {
			return nil, err
		}
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content:          content.String(),
		ReasoningContent: response.Message.Thinking,
		StopReason:       response.DoneReason,
		GenerationInfo: map[string]any{
			"CompletionTokens": response.EvalCount,
			"PromptTokens":     response.PromptEvalCount,
			"TotalTokens":      response.EvalCount + response.PromptEvalCount,
		},
	}}}, nil
}

func ollamaVisionMessages(messages []llms.MessageContent) ([]ollamaapi.Message, error) {
	result := make([]ollamaapi.Message, 0, len(messages))
	for _, message := range messages {
		var content strings.Builder
		var images []ollamaapi.ImageData
		for _, part := range message.Parts {
			switch typed := part.(type) {
			case llms.TextContent:
				content.WriteString(typed.Text)
			case llms.BinaryContent:
				images = append(images, ollamaapi.ImageData(typed.Data))
			default:
				return nil, fmt.Errorf("Ollama vision model does not support %T message parts", part)
			}
		}
		result = append(result, ollamaapi.Message{
			Role:    ollamaVisionRole(message.Role),
			Content: content.String(),
			Images:  images,
		})
	}
	return result, nil
}

func ollamaVisionRole(role llms.ChatMessageType) string {
	switch role {
	case llms.ChatMessageTypeSystem:
		return "system"
	case llms.ChatMessageTypeAI:
		return "assistant"
	default:
		return "user"
	}
}

func isGLMOCRModel(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if separator := strings.IndexAny(name, ":@"); separator >= 0 {
		name = name[:separator]
	}
	return strings.HasPrefix(name, "glm-ocr")
}
