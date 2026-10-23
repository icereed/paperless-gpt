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
	"github.com/tmc/langchaingo/llms"
)

// OLLAMA_THINK has to arrive as a top-level chat field. A value nested under
// options is ignored, and false is omitted by the vision client, so a thinking
// model keeps the transcription out of message.content.
func TestOllamaVisionSendsThinkAtTopLevel(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		want any
		set  bool
	}{
		{name: "false is transmitted", env: "false", want: false, set: true},
		{name: "true is transmitted", env: "true", want: true, set: true},
		{name: "reasoning level is transmitted", env: "high", want: "high", set: true},
		{name: "unset leaves the model default", set: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/chat", r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"invoice"},"done":true,"done_reason":"stop"}`+"\n")
			}))
			t.Cleanup(server.Close)

			t.Setenv("OLLAMA_HOST", server.URL)
			if tc.set {
				t.Setenv("OLLAMA_THINK", tc.env)
			} else {
				t.Setenv("OLLAMA_THINK", "")
			}

			model, err := createOllamaClient(Config{VisionLLMModel: "vision-model"})
			require.NoError(t, err)
			_, err = model.GenerateContent(context.Background(), []llms.MessageContent{
				llms.TextParts(llms.ChatMessageTypeHuman, "Read the page"),
			})
			require.NoError(t, err)

			if !tc.set {
				assert.NotContains(t, request, "think")
				return
			}
			assert.Equal(t, tc.want, request["think"])
			if options, ok := request["options"].(map[string]any); ok {
				assert.NotContains(t, options, "think")
			}
		})
	}
}

func TestWithOllamaThinkLiftsANestedValue(t *testing.T) {
	body := []byte(`{"model":"m","messages":[],"options":{"temperature":0,"think":true}}`)
	got, err := withOllamaThink(body, json.RawMessage("false"))
	require.NoError(t, err)

	var request map[string]any
	require.NoError(t, json.Unmarshal(got, &request))
	assert.Equal(t, false, request["think"])
	options := request["options"].(map[string]any)
	assert.NotContains(t, options, "think")
	assert.Equal(t, 0.0, options["temperature"])
}
