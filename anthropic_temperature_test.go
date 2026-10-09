package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/anthropic"
)

// Claude models from Sonnet 5 / Opus 4.7 on reject any request that carries a
// temperature field (issue #1003). Title, tag and metadata generation must not
// send one for those models, while older models keep the explicit default.
func TestSuggestionLLM_AnthropicTemperature(t *testing.T) {
	tests := []struct {
		model           string
		wantTemperature bool
	}{
		{model: "claude-sonnet-5", wantTemperature: false},
		{model: "claude-sonnet-5-5", wantTemperature: false},
		{model: "claude-opus-4-7", wantTemperature: false},
		{model: "claude-haiku-4-5", wantTemperature: true},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(raw, &body))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",` +
					`"content":[{"type":"text","text":"Invoice 42"}],"stop_reason":"end_turn",` +
					`"usage":{"input_tokens":1,"output_tokens":1}}`))
			}))
			defer srv.Close()

			llm, err := anthropic.New(
				anthropic.WithModel(tt.model),
				anthropic.WithToken("test-key"),
				anthropic.WithBaseURL(srv.URL),
			)
			require.NoError(t, err)
			app := &App{LLM: NewRateLimitedLLM(llm, getRateLimitConfig(false))}

			tmpl := template.Must(template.New("title").Parse("{{.Content}}"))
			title, err := app.getSuggestedTitle(context.Background(), "Invoice 42 content", "scan.pdf", logrus.WithField("test", t.Name()), tmpl)
			require.NoError(t, err)
			assert.Equal(t, "Invoice 42", title)

			_, hasTemperature := body["temperature"]
			assert.Equal(t, tt.wantTemperature, hasTemperature, "request body: %v", body)
		})
	}
}
