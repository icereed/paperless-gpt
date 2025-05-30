package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// Structured output response schemas
type TitleResponse struct {
	Title string `json:"title"`
}

type TagsResponse struct {
	Tags []string `json:"tags"`
}

type StructuredCorrespondentResponse struct {
	Correspondent string `json:"correspondent"`
}

type CreatedDateResponse struct {
	CreatedDate string `json:"created_date"`
}

// isStructuredOutputEnabled checks if structured output should be used
func isStructuredOutputEnabled() bool {
	return ollamaStructuredOutput && strings.ToLower(llmProvider) == "ollama"
}

// callLLMWithStructuredOutput makes an LLM call with optional structured output
func (app *App) callLLMWithStructuredOutput(ctx context.Context, prompt string, useStructured bool, schema interface{}) (*llms.ContentResponse, error) {
	messages := []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	}

	var options []llms.CallOption
	if useStructured && schema != nil {
		// Add structured output options for Ollama
		options = append(options, llms.WithJSONMode())
	}

	return app.LLM.GenerateContent(ctx, messages, options...)
}

// parseStructuredResponse attempts to parse JSON response, falls back to text if needed
func parseStructuredResponse(response string, target interface{}) error {
	// Try to parse as JSON first
	if err := json.Unmarshal([]byte(response), target); err != nil {
		return fmt.Errorf("failed to parse structured response: %w", err)
	}
	return nil
}
