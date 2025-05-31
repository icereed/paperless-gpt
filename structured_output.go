package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// Structured output response schemas
type TitleResponse struct {
	Think *string `json:"think,omitempty"` // Optional reasoning field to help models produce better results
	Title string  `json:"title"`
}

type TagsResponse struct {
	Think *string  `json:"think,omitempty"` // Optional reasoning field to help models produce better results
	Tags  []string `json:"tags"`
}

type StructuredCorrespondentResponse struct {
	Think         *string `json:"think,omitempty"` // Optional reasoning field to help models produce better results
	Correspondent string  `json:"correspondent"`
}

type CreatedDateResponse struct {
	Think       *string `json:"think,omitempty"` // Optional reasoning field to help models produce better results
	CreatedDate string  `json:"created_date"`
}

type OCRResponse struct {
	IntroComment  *string `json:"intro_comment,omitempty"`  // Optional initial thoughts about the document
	Content       string  `json:"content"`                  // The actual transcribed text content
	FinishComment *string `json:"finish_comment,omitempty"` // Optional final observations
}

// isStructuredOutputEnabled checks if structured output should be used
func isStructuredOutputEnabled() bool {
	if ollamaStructuredOutput && strings.ToLower(llmProvider) == "ollama" {
		return true
	}
	
	// Enable for OpenAI and other providers that support JSON mode
	if strings.ToLower(llmProvider) == "openai" || strings.ToLower(llmProvider) == "mistral" {
		return os.Getenv("STRUCTURED_OUTPUT_ENABLED") == "true"
	}
	
	return false
}

// callLLMWithStructuredOutput makes a text-only LLM call with optional structured output
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
		options = append(options, llms.WithJSONMode())
		
		// For providers that support strict schema enforcement
		if structuredOutputStrict {
			// Add schema validation if the provider supports it
			// This would need provider-specific implementation based on the provider
			switch strings.ToLower(llmProvider) {
			case "openai":
				// OpenAI supports strict schema enforcement in some cases
				// This would require additional OpenAI-specific options
			default:
				// Other providers may implement schema validation differently
			}
		}
	}

	return app.LLM.GenerateContent(ctx, messages, options...)
}

// callVisionLLMWithStructuredOutput makes a vision LLM call with image and text content, with optional structured output
func (app *App) callVisionLLMWithStructuredOutput(ctx context.Context, prompt string, imageData []byte, useStructured bool, schema interface{}) (*llms.ContentResponse, error) {
	if app.VisionLLM == nil {
		return nil, fmt.Errorf("vision LLM is not configured")
	}

	parts := []llms.ContentPart{
		llms.TextContent{
			Text: prompt,
		},
	}

	// Add image content if provided
	if len(imageData) > 0 {
		parts = append(parts, llms.ImageURLContent{
			URL: fmt.Sprintf("data:image/jpeg;base64,%s", encodeImageToBase64(imageData)),
		})
	}

	messages := []llms.MessageContent{
		{
			Parts: parts,
			Role:  llms.ChatMessageTypeHuman,
		},
	}

	var options []llms.CallOption
	if useStructured && schema != nil {
		options = append(options, llms.WithJSONMode())
		
		// For providers that support strict schema enforcement
		if structuredOutputStrict {
			// Add schema validation if the provider supports it
			// This would need provider-specific implementation based on the vision provider
			switch strings.ToLower(visionLlmProvider) {
			case "openai":
				// OpenAI supports strict schema enforcement in some cases
				// This would require additional OpenAI-specific options
			default:
				// Other providers may implement schema validation differently
			}
		}
	}

	return app.VisionLLM.GenerateContent(ctx, messages, options...)
}

// parseStructuredResponse attempts to parse JSON response, falls back to text if needed
func parseStructuredResponse(response string, target interface{}) error {
	// Try to parse as JSON first
	if err := json.Unmarshal([]byte(response), target); err != nil {
		return fmt.Errorf("failed to parse structured response: %w", err)
	}
	return nil
}

// encodeImageToBase64 encodes image data to base64 string
func encodeImageToBase64(imageData []byte) string {
	return base64.StdEncoding.EncodeToString(imageData)
}
