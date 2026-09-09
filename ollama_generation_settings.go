package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	ollamaapi "github.com/ollama/ollama/api"
)

// OllamaMetadataConfig configures metadata generation requests sent to native
// Ollama. Prompt entries are keyed by the prompt filename selected by the
// caller.
type OllamaMetadataConfig struct {
	Defaults OllamaGenerationSettings            `json:"defaults,omitempty"`
	Prompts  map[string]OllamaGenerationSettings `json:"prompts,omitempty"`
}

// OllamaGenerationSettings contains optional Ollama chat generation settings.
// Pointers intentionally distinguish an omitted setting from an explicit zero
// value. Think and Format are raw JSON because Ollama accepts more than one
// JSON type for each of them.
type OllamaGenerationSettings struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	ContextLength    *int            `json:"num_ctx,omitempty"`
	KeepAlive        *string         `json:"keep_alive,omitempty"`
	Think            json.RawMessage `json:"think,omitempty"`
	Format           json.RawMessage `json:"format,omitempty"`
	TopK             *int            `json:"top_k,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	MinP             *float64        `json:"min_p,omitempty"`
	Seed             *int            `json:"seed,omitempty"`
	RepeatPenalty    *float64        `json:"repeat_penalty,omitempty"`
	RepeatLastN      *int            `json:"repeat_last_n,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Stop             *[]string       `json:"stop,omitempty"`
}

// Validate checks syntax and request-level invariants. Ollama remains
// responsible for validating a JSON schema's semantics.
func (s OllamaGenerationSettings) Validate() error {
	for _, value := range []*float64{s.Temperature, s.TopP, s.MinP, s.RepeatPenalty, s.FrequencyPenalty, s.PresencePenalty} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return fmt.Errorf("Ollama generation setting must be a finite number")
		}
	}
	if s.Temperature != nil && *s.Temperature < 0 {
		return fmt.Errorf("Ollama temperature must be non-negative")
	}
	if s.MaxTokens != nil && (*s.MaxTokens == 0 || *s.MaxTokens < -1) {
		return fmt.Errorf("Ollama max_tokens must be positive or -1")
	}
	if s.ContextLength != nil && *s.ContextLength <= 0 {
		return fmt.Errorf("Ollama num_ctx must be positive")
	}
	if s.TopK != nil && *s.TopK < 0 {
		return fmt.Errorf("Ollama top_k must be non-negative")
	}
	if s.TopP != nil && (*s.TopP < 0 || *s.TopP > 1) {
		return fmt.Errorf("Ollama top_p must be between 0 and 1")
	}
	if s.MinP != nil && (*s.MinP < 0 || *s.MinP > 1) {
		return fmt.Errorf("Ollama min_p must be between 0 and 1")
	}
	if s.RepeatPenalty != nil && *s.RepeatPenalty < 0 {
		return fmt.Errorf("Ollama repeat_penalty must be non-negative")
	}
	if s.RepeatLastN != nil && *s.RepeatLastN < -1 {
		return fmt.Errorf("Ollama repeat_last_n must be -1 or greater")
	}
	if s.KeepAlive != nil {
		if _, err := parseOllamaKeepAlive(*s.KeepAlive); err != nil {
			return err
		}
	}
	if len(s.Think) > 0 {
		if _, err := ollamaThinkValue(s.Think); err != nil {
			return err
		}
	}
	if len(s.Format) > 0 {
		if err := validateOllamaFormat(s.Format); err != nil {
			return err
		}
	}
	return nil
}

func validateOllamaMetadataConfig(config OllamaMetadataConfig) error {
	if err := config.Defaults.Validate(); err != nil {
		return fmt.Errorf("validate Ollama defaults: %w", err)
	}
	for filename, settings := range config.Prompts {
		if err := settings.Validate(); err != nil {
			return fmt.Errorf("validate Ollama prompt %q: %w", filename, err)
		}
	}
	return nil
}

func parseOllamaKeepAlive(value string) (*ollamaapi.Duration, error) {
	if value == "0" {
		return &ollamaapi.Duration{Duration: 0}, nil
	}
	if value == "-1" {
		return &ollamaapi.Duration{Duration: -1}, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return nil, fmt.Errorf("invalid Ollama keep_alive %q", value)
	}
	return &ollamaapi.Duration{Duration: duration}, nil
}

func ollamaThinkValue(raw json.RawMessage) (*ollamaapi.ThinkValue, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid Ollama think: %w", err)
	}
	switch typed := value.(type) {
	case bool:
		return &ollamaapi.ThinkValue{Value: typed}, nil
	case string:
		if typed == "low" || typed == "medium" || typed == "high" {
			return &ollamaapi.ThinkValue{Value: typed}, nil
		}
	}
	return nil, fmt.Errorf("invalid Ollama think (expected true, false, low, medium, or high)")
}

func validateOllamaFormat(raw json.RawMessage) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("invalid Ollama format: %w", err)
	}
	if value == "json" {
		return nil
	}
	if schema, ok := value.(map[string]any); ok && len(schema) > 0 {
		return nil
	}
	return fmt.Errorf("invalid Ollama format (expected \"json\" or a non-empty JSON object)")
}

func (s OllamaGenerationSettings) clone() OllamaGenerationSettings {
	result := OllamaGenerationSettings{
		Temperature: cloneFloat(s.Temperature), MaxTokens: cloneInt(s.MaxTokens), ContextLength: cloneInt(s.ContextLength), KeepAlive: cloneString(s.KeepAlive),
		TopK: cloneInt(s.TopK), TopP: cloneFloat(s.TopP), MinP: cloneFloat(s.MinP), Seed: cloneInt(s.Seed), RepeatPenalty: cloneFloat(s.RepeatPenalty),
		RepeatLastN: cloneInt(s.RepeatLastN), FrequencyPenalty: cloneFloat(s.FrequencyPenalty), PresencePenalty: cloneFloat(s.PresencePenalty),
	}
	if s.Think != nil {
		result.Think = append(json.RawMessage(nil), s.Think...)
	}
	if s.Format != nil {
		result.Format = append(json.RawMessage(nil), s.Format...)
	}
	if s.Stop != nil {
		copied := make([]string, len(*s.Stop))
		copy(copied, *s.Stop)
		result.Stop = &copied
	}
	return result
}

func (s OllamaGenerationSettings) overlay(overrides OllamaGenerationSettings) OllamaGenerationSettings {
	result := s.clone()
	if overrides.Temperature != nil {
		result.Temperature = cloneFloat(overrides.Temperature)
	}
	if overrides.MaxTokens != nil {
		result.MaxTokens = cloneInt(overrides.MaxTokens)
	}
	if overrides.ContextLength != nil {
		result.ContextLength = cloneInt(overrides.ContextLength)
	}
	if overrides.KeepAlive != nil {
		result.KeepAlive = cloneString(overrides.KeepAlive)
	}
	if overrides.Think != nil {
		result.Think = append(json.RawMessage(nil), overrides.Think...)
	}
	if overrides.Format != nil {
		result.Format = append(json.RawMessage(nil), overrides.Format...)
	}
	if overrides.TopK != nil {
		result.TopK = cloneInt(overrides.TopK)
	}
	if overrides.TopP != nil {
		result.TopP = cloneFloat(overrides.TopP)
	}
	if overrides.MinP != nil {
		result.MinP = cloneFloat(overrides.MinP)
	}
	if overrides.Seed != nil {
		result.Seed = cloneInt(overrides.Seed)
	}
	if overrides.RepeatPenalty != nil {
		result.RepeatPenalty = cloneFloat(overrides.RepeatPenalty)
	}
	if overrides.RepeatLastN != nil {
		result.RepeatLastN = cloneInt(overrides.RepeatLastN)
	}
	if overrides.FrequencyPenalty != nil {
		result.FrequencyPenalty = cloneFloat(overrides.FrequencyPenalty)
	}
	if overrides.PresencePenalty != nil {
		result.PresencePenalty = cloneFloat(overrides.PresencePenalty)
	}
	if overrides.Stop != nil {
		copied := make([]string, len(*overrides.Stop))
		copy(copied, *overrides.Stop)
		result.Stop = &copied
	}
	return result
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func (config OllamaMetadataConfig) clone() OllamaMetadataConfig {
	result := OllamaMetadataConfig{Defaults: config.Defaults.clone()}
	if config.Prompts != nil {
		result.Prompts = make(map[string]OllamaGenerationSettings, len(config.Prompts))
		for filename, settings := range config.Prompts {
			result.Prompts[filename] = settings.clone()
		}
	}
	return result
}

type ollamaPromptContextKey struct{}

// withOllamaPrompt attaches the selected prompt filename to a request context.
// Its unexported key avoids collisions with contexts owned by callers.
func withOllamaPrompt(ctx context.Context, filename string) context.Context {
	return context.WithValue(ctx, ollamaPromptContextKey{}, strings.TrimSpace(filename))
}

func ollamaPromptFromContext(ctx context.Context) string {
	filename, _ := ctx.Value(ollamaPromptContextKey{}).(string)
	return filename
}
