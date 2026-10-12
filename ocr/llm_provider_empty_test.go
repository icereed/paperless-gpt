package ocr

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/openai"
)

// A thinking model behind an OpenAI-compatible server (e.g. Qwen in LM Studio)
// can spend its whole token budget on reasoning and return empty content with
// finish_reason "length". The page must come back blank rather than fail the
// document, and the log must say why it is blank.
func TestProcessImage_EmptyContentFromTokenLimitIsBlankPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"qwen3.5-9b","choices":[{"index":0,` +
			`"message":{"role":"assistant","content":"","reasoning_content":"Let me look at the image..."},` +
			`"finish_reason":"length"}],"usage":{"prompt_tokens":900,"completion_tokens":4096,"total_tokens":4996}}`))
	}))
	defer srv.Close()

	llm, err := openai.New(openai.WithModel("qwen3.5-9b"), openai.WithToken("test-key"), openai.WithBaseURL(srv.URL))
	require.NoError(t, err)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(logrus.StandardLogger().Out)

	provider := &LLMProvider{provider: "openai", model: "qwen3.5-9b", llm: llm, prompt: "OCR this"}
	result, err := provider.ProcessImage(context.Background(), testJPEG(t), 3)

	require.NoError(t, err)
	assert.Empty(t, result.Text)
	assert.Contains(t, logs.String(), "Vision model returned no text for this page")
	assert.Contains(t, logs.String(), "stop_reason=length")
}
