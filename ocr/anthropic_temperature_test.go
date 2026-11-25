package ocr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/anthropic"
)

// newAnthropicCaptureServer returns a fake Anthropic Messages endpoint that
// records the decoded body of every request it receives.
func newAnthropicCaptureServer(t *testing.T, bodies *[]map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		*bodies = append(*bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ocr text"}],"stop_reason":"end_turn",` +
			`"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Claude models from Sonnet 5 / Opus 4.7 on reject any request that carries a
// temperature field (issue #1003). The vision OCR path must not send one for
// those models, even when VISION_LLM_TEMPERATURE is set, while older models
// keep receiving the configured value.
func TestProcessImage_AnthropicTemperature(t *testing.T) {
	zero := 0.0
	tests := []struct {
		name            string
		model           string
		temperature     *float64
		wantTemperature bool
	}{
		{name: "sonnet 5 without temperature", model: "claude-sonnet-5", wantTemperature: false},
		{name: "sonnet 5.5 without temperature", model: "claude-sonnet-5-5", wantTemperature: false},
		{name: "sonnet 5 with VISION_LLM_TEMPERATURE", model: "claude-sonnet-5", temperature: &zero, wantTemperature: false},
		{name: "opus 4.7", model: "claude-opus-4-7", temperature: &zero, wantTemperature: false},
		{name: "haiku 4.5 keeps temperature", model: "claude-haiku-4-5", temperature: &zero, wantTemperature: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodies []map[string]any
			srv := newAnthropicCaptureServer(t, &bodies)

			llm, err := anthropic.New(
				anthropic.WithModel(tt.model),
				anthropic.WithToken("test-key"),
				anthropic.WithBaseURL(srv.URL),
			)
			require.NoError(t, err)

			provider := &LLMProvider{
				provider:    "anthropic",
				model:       tt.model,
				llm:         llm,
				prompt:      "OCR this",
				temperature: tt.temperature,
			}
			result, err := provider.ProcessImage(context.Background(), testJPEG(t), 1)
			require.NoError(t, err)
			assert.Equal(t, "ocr text", result.Text)

			require.Len(t, bodies, 1)
			_, hasTemperature := bodies[0]["temperature"]
			assert.Equal(t, tt.wantTemperature, hasTemperature, "request body: %v", bodies[0])
		})
	}
}
