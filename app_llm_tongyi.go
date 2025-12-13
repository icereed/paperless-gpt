package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"net/http"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// TongyiProvider implements llms.Model for 通义千问 (Tongyi)
type TongyiProvider struct {
	apiKey   string
	endpoint string
	model    string
	client   *http.Client
}

// NewTongyiProvider creates a new TongyiProvider. endpoint should be the base URL of the API (no trailing path required).
func NewTongyiProvider(apiKey, endpoint, model string) (*TongyiProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("TONGYI_API_KEY not set")
	}
	if endpoint == "" {
		// Default placeholder endpoint; users should override with TONGYI_ENDPOINT
		endpoint = "https://api.tongyi.example"
	}
	if model == "" {
		model = "default"
	}

	return &TongyiProvider{
		apiKey:   apiKey,
		endpoint: endpoint,
		model:    model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// GenerateText sends a text-generation request to the Tongyi API and returns the resulting text.
func (p *TongyiProvider) GenerateText(ctx context.Context, prompt string) (string, error) {
	// Construct request body following the Dashscope/Tongyi compatible chat completions shape.
	// We send the prompt as a single user message with a text content part.
	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": prompt},
				},
			},
		},
		"stream": false,
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// The Dashscope-compatible API exposes a chat completions endpoint.
	url := strings.TrimRight(p.endpoint, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tongyi request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("tongyi API error status: %d", resp.StatusCode)
	}

	// Parse response. Compatible responses may include nested structures.
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("failed to decode tongyi response: %w", err)
	}

	// Try to extract text from common locations in the response
	if choices, ok := data["choices"].([]interface{}); ok && len(choices) > 0 {
		first := choices[0].(map[string]interface{})

		// 1) message.content -> array of {type,text}
		if message, ok := first["message"].(map[string]interface{}); ok {
			// Some Tongyi-compatible responses put the combined text directly in
			// `message.content` as a string. Check that first.
			if contentStr, ok := message["content"].(string); ok && contentStr != "" {
				return contentStr, nil
			}

			if contentArr, ok := message["content"].([]interface{}); ok {
				var out strings.Builder
				for _, part := range contentArr {
					if pm, ok := part.(map[string]interface{}); ok {
						if t, ok := pm["text"].(string); ok {
							out.WriteString(t)
						}
					}
				}
				if out.Len() > 0 {
					return out.String(), nil
				}
			}
		}

		// 2) choices[0].text (older style)
		if txt, ok := first["text"].(string); ok && txt != "" {
			return txt, nil
		}

		// 3) choices[0].delta.content (streaming chunk fallback)
		if delta, ok := first["delta"].(map[string]interface{}); ok {
			if c, ok := delta["content"].(string); ok && c != "" {
				return c, nil
			}
		}
	}

	// If we reach here, parsing failed — log the raw response for debugging
	// Use debug level to avoid leaking in normal logs; can be removed after diagnosis
	log.Debugf("Tongyi raw response: %+v", data)

	return "", fmt.Errorf("no usable text found in Tongyi response")
}

// GenerateContent implements llms.Model by adapting messages to a single prompt.
func (p *TongyiProvider) GenerateContent(ctx context.Context, messages []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	if len(messages) == 0 || len(messages[0].Parts) == 0 {
		return nil, fmt.Errorf("no prompt provided")
	}
	textPart, ok := messages[0].Parts[0].(llms.TextContent)
	if !ok {
		return nil, fmt.Errorf("first message part is not TextContent")
	}
	result, err := p.GenerateText(ctx, textPart.Text)
	if err != nil {
		return nil, err
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{Content: result}},
	}, nil
}

// Call implements the simple call interface used by langchaingo
func (p *TongyiProvider) Call(ctx context.Context, prompt string, opts ...llms.CallOption) (string, error) {
	return p.GenerateText(ctx, prompt)
}

// Close implements optional cleanup
func (p *TongyiProvider) Close() error {
	return nil
}

// ProviderName returns provider identifier
func (p *TongyiProvider) ProviderName() string { return "tongyi" }
