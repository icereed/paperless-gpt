package ocr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessImageOllamaRequestsAreNonStreaming(t *testing.T) {
	tests := []struct {
		model          string
		wantStream     any
		responseIsDone bool
	}{
		{model: "glm-ocr:latest", wantStream: false, responseIsDone: false},
		{model: "qwen2.5vl:3b", wantStream: nil, responseIsDone: true},
		{model: "minicpm-v:8b", wantStream: nil, responseIsDone: true},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			temperature := 0.0
			topK := 40
			config := Config{
				VisionLLMProvider: "ollama",
				VisionLLMModel:    test.model,
				VisionLLMPrompt:   "Text Recognition:",
			}
			if test.model == "glm-ocr:latest" {
				config.VisionLLMMaxTokens = 1000
				config.VisionLLMTemperature = &temperature
				config.OllamaOcrTopK = &topK
				config.OllamaContextLength = 8192
			}

			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/chat", r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

				w.Header().Set("Content-Type", "application/x-ndjson")
				response := map[string]any{
					"model":             test.model,
					"message":           map[string]any{"role": "assistant", "content": "recognized text"},
					"done":              test.responseIsDone,
					"prompt_eval_count": 7,
					"eval_count":        3,
				}
				if test.responseIsDone {
					response["done_reason"] = "stop"
				}
				err := json.NewEncoder(w).Encode(response)
				require.NoError(t, err)
			}))
			defer server.Close()

			t.Setenv("OLLAMA_HOST", server.URL)
			provider, err := newLLMProvider(config)
			require.NoError(t, err)

			result, err := provider.ProcessImage(context.Background(), testJPEG(t), 1)
			require.NoError(t, err)
			assert.Equal(t, "recognized text", result.Text)
			assert.Equal(t, test.model, request["model"])
			assert.Equal(t, test.wantStream, request["stream"])
			if test.model == "glm-ocr:latest" {
				requestOptions := request["options"].(map[string]any)
				assert.Equal(t, float64(8192), requestOptions["num_ctx"])
				assert.Equal(t, float64(1000), requestOptions["num_predict"])
				assert.Equal(t, float64(0), requestOptions["temperature"])
				assert.Equal(t, float64(40), requestOptions["top_k"])
			}
		})
	}
}

func TestIsGLMOCRModel(t *testing.T) {
	for _, model := range []string{"glm-ocr", "glm-ocr:bf16", "library/glm-ocr:q8_0", "GLM-OCR@sha256:abc"} {
		assert.True(t, isGLMOCRModel(model), model)
	}
	for _, model := range []string{"qwen2.5vl:3b", "minicpm-v:8b", "my-glm-ocr:latest"} {
		assert.False(t, isGLMOCRModel(model), model)
	}
}
