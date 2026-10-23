package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/anthropic"
)

// Claude models that think by default (adaptive thinking, e.g. claude-haiku-5-5
// and claude-sonnet-5-5) reply with a thinking block before the text block, and
// with the default thinking display "omitted" that block's text is empty.
// langchaingo maps each block to its own choice, so reading Choices[0] returned
// the empty thinking block: every field came back as "no suggestion", without
// an error, whenever the model decided to think. The answer is the text block.
func TestSuggestions_AnthropicThinkingBlockBeforeText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5",` +
			`"content":[{"type":"thinking","thinking":"","signature":"sig"},{"type":"text","text":"Invoice"}],` +
			`"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":40}}`))
	}))
	defer srv.Close()

	llm, err := anthropic.New(
		anthropic.WithModel("claude-haiku-5-5"),
		anthropic.WithToken("test-key"),
		anthropic.WithBaseURL(srv.URL),
	)
	require.NoError(t, err)
	app := &App{LLM: NewRateLimitedLLM(llm, RateLimitConfig{MaxRetries: 3, BackoffMaxWait: time.Second})}
	logger := logrus.WithField("test", t.Name())
	tmpl := template.Must(template.New("prompt").Parse("{{.Content}}"))

	title, err := app.getSuggestedTitle(context.Background(), "invoice text", "scan.pdf", logger, tmpl)
	require.NoError(t, err)
	assert.Equal(t, "Invoice", title)

	docType, err := app.getSuggestedDocumentType(context.Background(), "invoice text", "", []string{"Invoice", "Letter"}, logger, tmpl)
	require.NoError(t, err)
	assert.Equal(t, "Invoice", docType)

	tags, err := app.getSuggestedTags(context.Background(), "invoice text", "", []string{"Invoice", "Receipt"}, nil, logger, tmpl)
	require.NoError(t, err)
	assert.Equal(t, []string{"Invoice"}, tags)
}
