package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// Use the real pinned SDK: this reproduces its default temperature=0 for
// both metadata requests and the identical client used by vision OCR.
func TestGPT6OpenAIClientTemperature(t *testing.T) {
	for _, tc := range []struct {
		model           string
		wantTemperature bool
	}{
		{"gpt-6.1-terra", false},
		{"gpt-6.1-terra-2026-10-01", false},
		{"gpt-6-luna", false},
		{"gpt-5.6-luna", false},
		{"gpt-4.1-mini", true},
	} {
		t.Run(tc.model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, hasTemperature := body["temperature"]
				assert.Equal(t, tc.wantTemperature, hasTemperature)
				assert.Equal(t, `"`+tc.model+`"`, string(body["model"]))
				assert.Equal(t, "paperless-gpt", r.Header.Get("X-Title"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			model, err := openai.New(openai.WithModel(tc.model), openai.WithToken("test-key"),
				openai.WithBaseURL(server.URL), openai.WithHTTPClient(OpenAIHTTPClient()))
			require.NoError(t, err)
			result, err := model.GenerateContent(context.Background(), []llms.MessageContent{
				llms.TextParts(llms.ChatMessageTypeHuman, "Transcribe the supplied document."),
			})
			require.NoError(t, err)
			require.Equal(t, "ok", result.Choices[0].Content)
		})
	}
}

func TestGPT6TransportPreservesPayloadAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		wantTemperature          bool
	}{
		{"reasoning", "POST", "/v1/chat/completions", `{"model":"gpt-6.1-terra","temperature":0,"messages":[{"role":"user","content":"private text"}],"max_completion_tokens":4096}`, false},
		{"none", "POST", "/v1/chat/completions", `{"model":"gpt-6.1-terra","reasoning_effort":"none","temperature":0}`, true},
		{"other model", "POST", "/v1/chat/completions", `{"model":"gpt-4.1-mini","temperature":0}`, true},
		{"other endpoint", "POST", "/v1/responses", `{"model":"gpt-6.1-terra","temperature":0}`, true},
		{"invalid JSON", "POST", "/v1/chat/completions", `{bad json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &gpt6Transport{base: gpt6RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				got, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, int64(len(got)), r.ContentLength)
				replay, err := r.GetBody()
				require.NoError(t, err)
				defer replay.Close()
				replayed, err := io.ReadAll(replay)
				require.NoError(t, err)
				require.Equal(t, got, replayed)
				if tc.name != "reasoning" {
					require.Equal(t, tc.body, string(got), "unaffected requests must remain byte-identical")
				} else {
					var before, after map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(tc.body), &before))
					require.NoError(t, json.Unmarshal(got, &after))
					delete(before, "temperature")
					require.Equal(t, before, after)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
			})}
			req, err := http.NewRequest(tc.method, "http://example.invalid"+tc.path, bytes.NewBufferString(tc.body))
			require.NoError(t, err)
			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			resp.Body.Close()
		})
	}
}

type gpt6RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f gpt6RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
