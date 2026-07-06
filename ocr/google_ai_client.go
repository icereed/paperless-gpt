package ocr

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"google.golang.org/genai"
)

// GoogleAIProvider implements the LLMProvider interface for Google Gemini API using google.golang.org/genai
type GoogleAIProvider struct {
	client         *genai.Client
	thinkingBudget *int32
	model          string
}

// NewGoogleAIProvider creates a new GoogleAIProvider instance
func NewGoogleAIProvider(ctx context.Context, model string, apiKey string, thinkingBudget *int32) (*GoogleAIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GOOGLEAI_API_KEY environment variable is not set")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create googleai client: %w", err)
	}

	return &GoogleAIProvider{
		client:         client,
		thinkingBudget: thinkingBudget,
		model:          model,
	}, nil
}

// GenerateText sends a text generation request to Gemini API
func (p *GoogleAIProvider) GenerateText(ctx context.Context, prompt string) (string, error) {
	var genConfig *genai.GenerateContentConfig
	if p.thinkingBudget != nil {
		genConfig = &genai.GenerateContentConfig{
			ThinkingConfig: &genai.ThinkingConfig{
				ThinkingBudget: genai.Ptr(*p.thinkingBudget),
			},
		}
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.model, genai.Text(prompt), genConfig)
	if err != nil {
		return "", fmt.Errorf("googleai GenerateContent API error: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return "", fmt.Errorf("googleai GenerateContent API returned empty response")
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return "", fmt.Errorf("googleai GenerateContent API returned a candidate with no content")
	}

	// Skip thinking parts when thinking budget is enabled
	for _, part := range candidate.Content.Parts {
		if !part.Thought && part.Text != "" {
			return part.Text, nil
		}
	}

	return "", fmt.Errorf("googleai GenerateContent API returned no non-thinking text parts")
}

// Close closes any resources held by the provider
func (p *GoogleAIProvider) Close() error {
	return nil
}

// GenerateContent implements the llms.Model interface, supporting text, binary and data URL image parts.
func (p *GoogleAIProvider) GenerateContent(ctx context.Context, messages []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no prompt provided")
	}

	createPart := func(part llms.ContentPart) (*genai.Part, error) {
		switch v := part.(type) {
		case llms.TextContent:
			return &genai.Part{Text: v.Text}, nil
		case llms.BinaryContent:
			mimeType := v.MIMEType
			if mimeType == "" {
				mimeType = "image/jpeg"
			}
			return &genai.Part{
				InlineData: &genai.Blob{
					Data:     v.Data,
					MIMEType: mimeType,
				},
			}, nil
		case llms.ImageURLContent:
			if strings.HasPrefix(v.URL, "data:") {
				parts := strings.Split(v.URL, ",")
				if len(parts) != 2 {
					return nil, fmt.Errorf("invalid data URL format")
				}
				meta := parts[0]
				dataBase64 := parts[1]

				mimeType := "image/jpeg"
				if strings.Contains(meta, ";") {
					mimeType = strings.TrimPrefix(strings.Split(meta, ";")[0], "data:")
				}

				data, err := base64.StdEncoding.DecodeString(dataBase64)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64 image: %w", err)
				}

				return &genai.Part{
					InlineData: &genai.Blob{
						Data:     data,
						MIMEType: mimeType,
					},
				}, nil
			}
			return nil, fmt.Errorf("unsupported ImageURLContent with non-data URL: %s", v.URL)
		default:
			return nil, fmt.Errorf("unsupported content part type: %T", v)
		}
	}

	var parts []*genai.Part
	for _, msg := range messages {
		for _, part := range msg.Parts {
			gp, err := createPart(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, gp)
		}
	}

	if len(parts) == 0 {
		return nil, fmt.Errorf("no valid content parts found")
	}

	// Prepare generation config with thinking budget if set
	var genConfig *genai.GenerateContentConfig
	if p.thinkingBudget != nil {
		genConfig = &genai.GenerateContentConfig{
			ThinkingConfig: &genai.ThinkingConfig{
				ThinkingBudget: genai.Ptr(*p.thinkingBudget),
			},
		}
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.model, []*genai.Content{{Parts: parts}}, genConfig)
	if err != nil {
		return nil, fmt.Errorf("googleai GenerateContent API error: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("googleai GenerateContent API returned empty response")
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return nil, fmt.Errorf("googleai GenerateContent API returned a candidate with no content")
	}

	// Concatenate non-thinking text parts
	var sb strings.Builder
	for _, part := range candidate.Content.Parts {
		if !part.Thought {
			sb.WriteString(part.Text)
		}
	}

	if sb.Len() == 0 {
		return nil, fmt.Errorf("googleai GenerateContent API returned no non-thinking text parts")
	}

	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: sb.String(),
			},
		},
	}, nil
}

// Call implements the llms.Model interface.
func (p *GoogleAIProvider) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return p.GenerateText(ctx, prompt)
}

