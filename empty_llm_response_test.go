package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

func TestRateLimitedLLM_GenerateContent_EmptyResponseIsAnAnswer(t *testing.T) {
	for _, emptyErr := range []error{anthropic.ErrEmptyResponse, openai.ErrEmptyResponse} {
		mockLLM := &rateLimitMockLLM{
			generateResponses: []*llms.ContentResponse{nil},
			generateErrors:    []error{emptyErr},
		}
		rateLimitedLLM := NewRateLimitedLLM(mockLLM, RateLimitConfig{MaxRetries: 3, BackoffMaxWait: time.Second})

		response, err := rateLimitedLLM.GenerateContent(context.Background(), []llms.MessageContent{
			llms.TextParts(llms.ChatMessageTypeHuman, "test message"),
		})

		require.NoError(t, err)
		require.Len(t, response.Choices, 1)
		assert.Empty(t, response.Choices[0].Content)
		assert.Equal(t, 1, mockLLM.generateIndex, "an empty reply must not be retried")
	}
}

// Claude can answer with an empty content array, e.g. for a document whose
// OCR text is just "67". langchaingo turns that into ErrEmptyResponse ("no
// response"), which used to be retried and then fail the whole document, so
// auto-processing ended with the fail tag. It must instead count as "no
// suggestion" for that field, like an empty reply from an OpenAI-compatible
// model already does.
func TestSuggestions_AnthropicEmptyContent(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
			`"content":[],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":1}}`))
	}))
	defer srv.Close()

	llm, err := anthropic.New(
		anthropic.WithModel("claude-haiku-4-5"),
		anthropic.WithToken("test-key"),
		anthropic.WithBaseURL(srv.URL),
	)
	require.NoError(t, err)
	app := &App{LLM: NewRateLimitedLLM(llm, RateLimitConfig{MaxRetries: 3, BackoffMaxWait: time.Second})}
	logger := logrus.WithField("test", t.Name())
	tmpl := template.Must(template.New("prompt").Parse("{{.Content}}"))

	title, err := app.getSuggestedTitle(context.Background(), "67", "scan.pdf", logger, tmpl)
	require.NoError(t, err)
	assert.Empty(t, title, "an empty title suggestion leaves the title unchanged")

	tags, err := app.getSuggestedTags(context.Background(), "67", "", []string{"Invoice", "Receipt"}, []string{"Receipt"}, logger, tmpl)
	require.NoError(t, err)
	assert.Equal(t, []string{"Receipt"}, tags, "the document keeps its tags")

	assert.Equal(t, int32(2), requests.Load(), "one request per field, no retries")
}
