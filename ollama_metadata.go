package main

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

// OllamaMetadataModel adapts Ollama's official client to the text-only
// metadata path. Vision/OCR continues to use its existing provider.
type OllamaMetadataModel struct {
	client        *ollamaapi.Client
	model         string
	contextLength int
	think         *bool
	temperature   float64
}

var _ llms.Model = (*OllamaMetadataModel)(nil)

func newOllamaMetadataModel(host, model string, contextLength int, think *bool, temperature float64, client *http.Client) (*OllamaMetadataModel, error) {
	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse Ollama host: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid Ollama host %q", host)
	}
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		return nil, fmt.Errorf("invalid Ollama temperature %v", temperature)
	}
	if client == nil {
		client = http.DefaultClient
	}

	return &OllamaMetadataModel{
		client:        ollamaapi.NewClient(baseURL, client),
		model:         model,
		contextLength: contextLength,
		think:         think,
		temperature:   temperature,
	}, nil
}

func (m *OllamaMetadataModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}

func (m *OllamaMetadataModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOptions := llms.CallOptions{Temperature: m.temperature}
	for _, option := range options {
		option(&callOptions)
	}
	if err := unsupportedOllamaOptions(callOptions); err != nil {
		return nil, err
	}

	chatMessages, err := ollamaMessages(messages)
	if err != nil {
		return nil, err
	}

	model := m.model
	if callOptions.Model != "" {
		model = callOptions.Model
	}

	stream := callOptions.StreamingFunc != nil || callOptions.StreamingReasoningFunc != nil
	request := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: chatMessages,
		Options:  ollamaOptions(callOptions, m.contextLength),
		Stream:   &stream,
	}
	if callOptions.JSONMode {
		request.Format = []byte(`"json"`)
	}
	if m.think != nil {
		request.Think = &ollamaapi.ThinkValue{Value: *m.think}
	}

	var content strings.Builder
	var reasoning strings.Builder
	var response ollamaapi.ChatResponse
	completed := false
	err = m.client.Chat(ctx, request, func(chunk ollamaapi.ChatResponse) error {
		if callOptions.StreamingFunc != nil {
			if err := callOptions.StreamingFunc(ctx, []byte(chunk.Message.Content)); err != nil {
				return err
			}
		}
		if callOptions.StreamingReasoningFunc != nil {
			if err := callOptions.StreamingReasoningFunc(ctx, []byte(chunk.Message.Thinking), []byte(chunk.Message.Content)); err != nil {
				return err
			}
		}

		content.WriteString(chunk.Message.Content)
		reasoning.WriteString(chunk.Message.Thinking)
		if chunk.Done {
			response = chunk
			completed = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("Ollama chat request: %w", err)
	}
	if !completed {
		return nil, fmt.Errorf("Ollama chat response ended before completion")
	}
	if content.Len() == 0 {
		return nil, fmt.Errorf("Ollama chat response contained no content")
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
		StopReason:       response.DoneReason,
		GenerationInfo: map[string]any{
			"CompletionTokens": response.EvalCount,
			"PromptTokens":     response.PromptEvalCount,
			"TotalTokens":      response.EvalCount + response.PromptEvalCount,
		},
	}}}, nil
}

func ollamaMessages(messages []llms.MessageContent) ([]ollamaapi.Message, error) {
	chatMessages := make([]ollamaapi.Message, 0, len(messages))
	for _, message := range messages {
		role, err := ollamaRole(message.Role)
		if err != nil {
			return nil, err
		}

		var content strings.Builder
		for _, part := range message.Parts {
			text, ok := part.(llms.TextContent)
			if !ok {
				return nil, fmt.Errorf("Ollama metadata model does not support %T message parts", part)
			}
			content.WriteString(text.Text)
		}
		chatMessages = append(chatMessages, ollamaapi.Message{Role: role, Content: content.String()})
	}
	return chatMessages, nil
}

func ollamaRole(role llms.ChatMessageType) (string, error) {
	switch role {
	case llms.ChatMessageTypeSystem:
		return "system", nil
	case llms.ChatMessageTypeAI:
		return "assistant", nil
	case llms.ChatMessageTypeHuman, llms.ChatMessageTypeGeneric:
		return "user", nil
	default:
		return "", fmt.Errorf("Ollama metadata model does not support %q messages", role)
	}
}

func ollamaOptions(options llms.CallOptions, contextLength int) map[string]any {
	result := map[string]any{"temperature": options.Temperature}
	if contextLength > 0 {
		result["num_ctx"] = contextLength
	}
	if options.MaxTokens != 0 {
		result["num_predict"] = options.MaxTokens
	}
	if len(options.StopWords) > 0 {
		result["stop"] = options.StopWords
	}
	if options.TopK != 0 {
		result["top_k"] = options.TopK
	}
	if options.TopP != 0 {
		result["top_p"] = options.TopP
	}
	if options.Seed != 0 {
		result["seed"] = options.Seed
	}
	if options.RepetitionPenalty != 0 {
		result["repeat_penalty"] = options.RepetitionPenalty
	}
	if options.FrequencyPenalty != 0 {
		result["frequency_penalty"] = options.FrequencyPenalty
	}
	if options.PresencePenalty != 0 {
		result["presence_penalty"] = options.PresencePenalty
	}
	return result
}

func unsupportedOllamaOptions(options llms.CallOptions) error {
	if options.CandidateCount > 1 || options.N > 1 {
		return fmt.Errorf("Ollama metadata model supports one response choice")
	}
	if len(options.Tools) > 0 || len(options.Functions) > 0 || options.ToolChoice != nil || options.FunctionCallBehavior != "" {
		return fmt.Errorf("Ollama metadata model does not support tool requests")
	}
	if options.MinLength != 0 || options.MaxLength != 0 || options.ResponseMIMEType != "" {
		return fmt.Errorf("Ollama metadata model does not support the requested generation option")
	}
	return nil
}
