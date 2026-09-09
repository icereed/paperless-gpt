package main

import (
	"context"
	"encoding/json"
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
	client *ollamaapi.Client
	model  string
	legacy OllamaGenerationSettings
	config OllamaMetadataConfig
}

var _ llms.Model = (*OllamaMetadataModel)(nil)

func newOllamaMetadataModel(host, model string, contextLength int, think *bool, temperature float64, client *http.Client, configured ...OllamaMetadataConfig) (*OllamaMetadataModel, error) {
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
	if len(configured) > 1 {
		return nil, fmt.Errorf("expected at most one Ollama metadata config")
	}
	config := OllamaMetadataConfig{}
	if len(configured) == 1 {
		config = configured[0]
	}
	if err := validateOllamaMetadataConfig(config); err != nil {
		return nil, err
	}
	legacy := OllamaGenerationSettings{Temperature: &temperature}
	if contextLength > 0 {
		legacy.ContextLength = &contextLength
	}
	if think != nil {
		legacy.Think = json.RawMessage("false")
		if *think {
			legacy.Think = json.RawMessage("true")
		}
	}

	return &OllamaMetadataModel{
		client: ollamaapi.NewClient(baseURL, client),
		model:  model,
		legacy: legacy,
		config: config.clone(),
	}, nil
}

func (m *OllamaMetadataModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, m, prompt, options...)
}

func (m *OllamaMetadataModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	callOptions := newOllamaCallOptionsSentinels()
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
	settings := m.legacy.overlay(m.config.Defaults)
	if promptSettings, ok := m.config.Prompts[ollamaPromptFromContext(ctx)]; ok {
		settings = settings.overlay(promptSettings)
	}
	settings = applyExplicitOllamaCallOptions(settings, callOptions)
	if err := settings.Validate(); err != nil {
		return nil, err
	}

	stream := callOptions.StreamingFunc != nil || callOptions.StreamingReasoningFunc != nil
	request := &ollamaapi.ChatRequest{
		Model:    model,
		Messages: chatMessages,
		Options:  ollamaGenerationOptions(settings),
		Stream:   &stream,
	}
	if settings.KeepAlive != nil {
		request.KeepAlive, err = parseOllamaKeepAlive(*settings.KeepAlive)
		if err != nil {
			return nil, err // Constructor already validates, but retain this for defensive use.
		}
	}
	if len(settings.Think) > 0 {
		request.Think, err = ollamaThinkValue(settings.Think)
		if err != nil {
			return nil, err
		}
	}
	if len(settings.Format) > 0 {
		request.Format = append(json.RawMessage(nil), settings.Format...)
	}
	if callOptions.JSONMode {
		request.Format = []byte(`"json"`)
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
	// A stopped completion can deliberately contain no assistant text. This is
	// used by the document-type prompt when none of the available types apply.
	// Keep rejecting an empty response unless Ollama explicitly reports its
	// normal stop reason: an absent reason is not enough to distinguish a
	// completed empty choice from an incomplete response, and "length" means
	// generation exhausted its budget before producing a choice.
	if content.Len() == 0 && response.DoneReason != "stop" {
		if response.DoneReason == "length" {
			return nil, fmt.Errorf("Ollama chat response reached its token limit before producing content")
		}
		return nil, fmt.Errorf("Ollama chat response contained no content (done_reason %q)", response.DoneReason)
	}
	if len(request.Format) > 0 && !json.Valid([]byte(content.String())) {
		return nil, fmt.Errorf("Ollama structured response was not valid JSON")
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

func ollamaGenerationOptions(settings OllamaGenerationSettings) map[string]any {
	result := make(map[string]any)
	if settings.Temperature != nil {
		result["temperature"] = *settings.Temperature
	}
	if settings.MaxTokens != nil {
		result["num_predict"] = *settings.MaxTokens
	}
	if settings.ContextLength != nil {
		result["num_ctx"] = *settings.ContextLength
	}
	if settings.TopK != nil {
		result["top_k"] = *settings.TopK
	}
	if settings.TopP != nil {
		result["top_p"] = *settings.TopP
	}
	if settings.MinP != nil {
		result["min_p"] = *settings.MinP
	}
	if settings.Seed != nil {
		result["seed"] = *settings.Seed
	}
	if settings.RepeatPenalty != nil {
		result["repeat_penalty"] = *settings.RepeatPenalty
	}
	if settings.RepeatLastN != nil {
		result["repeat_last_n"] = *settings.RepeatLastN
	}
	if settings.FrequencyPenalty != nil {
		result["frequency_penalty"] = *settings.FrequencyPenalty
	}
	if settings.PresencePenalty != nil {
		result["presence_penalty"] = *settings.PresencePenalty
	}
	if settings.Stop != nil {
		stop := make([]string, len(*settings.Stop))
		copy(stop, *settings.Stop)
		result["stop"] = stop
	}
	return result
}

// langchaingo CallOption is an opaque function and does not record which
// fields it changed. Apply options once to private sentinels rather than
// inferring presence from their eventual zero values. This supports standard
// llms.With* options, including explicit zero values. A custom option that
// intentionally assigns a private sentinel is unsupported.
const ollamaCallOptionIntSentinel = math.MinInt

const ollamaCallOptionStopSentinel = "\x00ollama-call-option-sentinel"

// Use a non-canonical NaN payload so normal NaN/+Inf CallOption values are
// detected and rejected by final validation rather than being mistaken for an
// untouched option.
var ollamaCallOptionFloatSentinel = math.Float64frombits(0x7ff80000000000f1)

func newOllamaCallOptionsSentinels() llms.CallOptions {
	return llms.CallOptions{
		Temperature:       ollamaCallOptionFloatSentinel,
		MaxTokens:         ollamaCallOptionIntSentinel,
		StopWords:         []string{ollamaCallOptionStopSentinel},
		TopK:              ollamaCallOptionIntSentinel,
		TopP:              ollamaCallOptionFloatSentinel,
		Seed:              ollamaCallOptionIntSentinel,
		RepetitionPenalty: ollamaCallOptionFloatSentinel,
		FrequencyPenalty:  ollamaCallOptionFloatSentinel,
		PresencePenalty:   ollamaCallOptionFloatSentinel,
	}
}

func applyExplicitOllamaCallOptions(settings OllamaGenerationSettings, options llms.CallOptions) OllamaGenerationSettings {
	if !isOllamaCallOptionFloatSentinel(options.Temperature) {
		settings.Temperature = cloneFloat(&options.Temperature)
	}
	if options.MaxTokens != ollamaCallOptionIntSentinel {
		if options.MaxTokens == 0 {
			// langchaingo uses zero for an unset limit. An explicit call can use
			// it to clear a configured num_predict without sending invalid zero.
			settings.MaxTokens = nil
		} else {
			settings.MaxTokens = cloneInt(&options.MaxTokens)
		}
	}
	if len(options.StopWords) != 1 || options.StopWords[0] != ollamaCallOptionStopSentinel {
		stop := make([]string, len(options.StopWords))
		copy(stop, options.StopWords)
		settings.Stop = &stop
	}
	if options.TopK != ollamaCallOptionIntSentinel {
		settings.TopK = cloneInt(&options.TopK)
	}
	if !isOllamaCallOptionFloatSentinel(options.TopP) {
		settings.TopP = cloneFloat(&options.TopP)
	}
	if options.Seed != ollamaCallOptionIntSentinel {
		settings.Seed = cloneInt(&options.Seed)
	}
	if !isOllamaCallOptionFloatSentinel(options.RepetitionPenalty) {
		settings.RepeatPenalty = cloneFloat(&options.RepetitionPenalty)
	}
	if !isOllamaCallOptionFloatSentinel(options.FrequencyPenalty) {
		settings.FrequencyPenalty = cloneFloat(&options.FrequencyPenalty)
	}
	if !isOllamaCallOptionFloatSentinel(options.PresencePenalty) {
		settings.PresencePenalty = cloneFloat(&options.PresencePenalty)
	}
	return settings
}

func isOllamaCallOptionFloatSentinel(value float64) bool {
	return math.Float64bits(value) == math.Float64bits(ollamaCallOptionFloatSentinel)
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
