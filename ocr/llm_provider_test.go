package ocr

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRetryAfterHTTPDateRoundsUp(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	delay, ok := parseRetryAfter("Sat, 10 Oct 2026 00:00:02 GMT", now.Add(500*time.Millisecond))
	require.True(t, ok)
	assert.Equal(t, 1500*time.Millisecond, delay)
}

func TestParseRetryAfterHTTPDateRoundingDoesNotOverflow(t *testing.T) {
	now := time.Unix(0, 0).UTC()
	when := now.Add(time.Duration(1<<63 - 1))
	delay, ok := parseRetryAfter(when.Format(http.TimeFormat), now)
	require.True(t, ok)
	assert.Greater(t, delay, time.Duration(0))
	assert.LessOrEqual(t, delay, time.Duration(1<<63-1))
}

func TestRetryAfterDelayRejectsOverflow(t *testing.T) {
	backoff := 5 * time.Millisecond
	assert.Equal(t, backoff, retryAfterDelay(errors.New("retry-after-ms:9223372036854775807"), backoff))
	assert.Equal(t, time.Second, retryAfterDelay(errors.New("retry-after-ms:1000"), backoff))
}

func TestParseHeaderList(t *testing.T) {
	got := ParseHeaderList(" A=1, B = two words ,C=x=y,broken,=nokey,D=")
	assert.Equal(t, map[string]string{"A": "1", "B": "two words", "C": "x=y", "D": ""}, got)
	assert.Empty(t, ParseHeaderList(""))
}

func TestParseHeaderListCanonicalizesNames(t *testing.T) {
	got := ParseHeaderList("x-title=a, X-TITLE=b")
	assert.Equal(t, map[string]string{"X-Title": "b"}, got, "names differing only in case are one header")
}

func TestHeaderClientDoesNotFollowRedirectsToOtherHosts(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
	}))
	defer other.Close()
	var sameHostHits int
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/v1", http.StatusFound) // same host: fine
		case "/v1":
			sameHostHits++
			assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		default:
			http.Redirect(w, r, other.URL, http.StatusFound) // another host
		}
	}))
	defer gateway.Close()

	client := headerClient(map[string]string{"Authorization": "Bearer secret"})

	resp, err := client.Get(gateway.URL + "/moved")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 1, sameHostHits, "redirects on the same host are followed")

	_, err = client.Get(gateway.URL + "/elsewhere")
	assert.ErrorContains(t, err, "refusing to follow a redirect")
	assert.Empty(t, leaked, "the credentials must not reach another host")
}

func TestHeaderClientPreservesRetryAfterOn429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	resp, err := headerClient(nil).Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Contains(t, string(body), "retry-after-ms:2000")
}

type recordingRoundTripper struct {
	body   []byte
	header http.Header
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.header = req.Header.Clone()
	if req.Body != nil {
		r.body, _ = io.ReadAll(req.Body)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}, Request: req}, nil
}

// The shared OpenAI-compatible client (text LLM, vision LLM, LLM OCR) adds
// both the configured headers and, with OPENROUTER_ENFORCE_ZDR, the
// zero-data-retention preference for requests to OpenRouter.
func TestOpenAIHTTPClientEnforcesOpenRouterZDR(t *testing.T) {
	t.Setenv("OPENROUTER_ENFORCE_ZDR", "true")
	t.Setenv("OPENAI_HEADERS", "X-Team=docs")
	rec := &recordingRoundTripper{}
	previous := http.DefaultTransport
	http.DefaultTransport = rec
	t.Cleanup(func() { http.DefaultTransport = previous })

	client := OpenAIHTTPClient()
	resp, err := client.Post("https://openrouter.ai/api/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	require.NoError(t, err)
	resp.Body.Close()

	var sent map[string]any
	require.NoError(t, json.Unmarshal(rec.body, &sent))
	provider, ok := sent["provider"].(map[string]any)
	require.True(t, ok, "the provider preference is added")
	assert.Equal(t, true, provider["zdr"])
	assert.Equal(t, "deny", provider["data_collection"])
	assert.Equal(t, "docs", rec.header.Get("X-Team"))
	assert.Equal(t, "paperless-gpt", rec.header.Get("X-Title"))

	// Other hosts are left alone.
	resp, err = client.Post("https://api.openai.com/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	require.NoError(t, err)
	resp.Body.Close()
	assert.JSONEq(t, `{"model":"m"}`, string(rec.body))
}
