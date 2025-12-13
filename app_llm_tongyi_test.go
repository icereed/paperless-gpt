package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestTongyiProvider_GenerateTextAndGenerateContent(t *testing.T) {
	// Mock Tongyi API (Dashscope-compatible chat completion response)
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// Return a chat-completions shaped response: choices[0].message.content[0].text
		resp := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"message": map[string]interface{}{
						"content": []interface{}{
							map[string]interface{}{"type": "text", "text": "mocked response"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}

	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	p, err := NewTongyiProvider("test-key", ts.URL, "test-model")
	if err != nil {
		t.Fatalf("failed to create Tongyi provider: %v", err)
	}

	ctx := context.Background()
	// Test GenerateText
	text, err := p.GenerateText(ctx, "hello")
	if err != nil {
		t.Fatalf("GenerateText failed: %v", err)
	}
	if text != "mocked response" {
		t.Fatalf("unexpected GenerateText result: %s", text)
	}

	// Test GenerateContent
	msg := llms.MessageContent{
		Parts: []llms.ContentPart{
			llms.TextContent{Text: "hello"},
		},
		Role: llms.ChatMessageTypeHuman,
	}

	resp, err := p.GenerateContent(ctx, []llms.MessageContent{msg})
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("GenerateContent returned empty response")
	}
	if resp.Choices[0].Content != "mocked response" {
		t.Fatalf("unexpected GenerateContent content: %s", resp.Choices[0].Content)
	}
}
