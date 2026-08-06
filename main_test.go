package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocument containing extra parameters for testing
type TestDocument struct {
	ID         int
	Title      string
	Tags       []string
	FailUpdate bool // simulate update failure
}

// Use this for TestCases in your tests
type TestCase struct {
	name           string
	documents      []TestDocument
	expectedCount  int
	expectedError  string
	updateResponse int // HTTP status code for update response
}

// Test our HTTP-Client
func TestCreateCustomHTTPClient(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom header
		assert.Equal(t, "paperless-gpt", r.Header.Get("X-Title"), "Expected X-Title header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Get custom client
	client := createCustomHTTPClient()
	require.NotNil(t, client, "HTTP client should not be nil")

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err, "Request should not fail")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK response")
}

func TestParseTagBlackList(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		// An unset variable must yield an inert list, not []string{""} — the
		// latter would blacklist the empty tag name and is the bug that
		// CORRESPONDENT_BLACK_LIST's naive strings.Split still carries.
		{name: "unset", raw: "", want: []string{}},
		{name: "whitespace only", raw: "   ", want: []string{}},
		{name: "single entry", raw: "paperless-gpt-failed", want: []string{"paperless-gpt-failed"}},
		{
			name: "spaces after commas are trimmed",
			raw:  "paperless-gpt-failed, paperless-gpt-auto-complete",
			want: []string{"paperless-gpt-failed", "paperless-gpt-auto-complete"},
		},
		{
			name: "empty entries are dropped",
			raw:  "a,,b,",
			want: []string{"a", "b"},
		},
		{
			name: "tag names may contain spaces",
			raw:  " needs review , done ",
			want: []string{"needs review", "done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseTagBlackList(tt.raw))
		})
	}
}

func TestRemoveBlackListedTags(t *testing.T) {
	tags := []string{"invoice", "paperless-gpt-failed", "receipt"}

	t.Run("empty blacklist returns input untouched", func(t *testing.T) {
		assert.Equal(t, tags, removeBlackListedTags(tags, []string{}))
		assert.Equal(t, tags, removeBlackListedTags(tags, nil))
	})

	t.Run("removes matching tags", func(t *testing.T) {
		assert.Equal(t, []string{"invoice", "receipt"},
			removeBlackListedTags(tags, []string{"paperless-gpt-failed"}))
	})

	t.Run("matching is case-insensitive", func(t *testing.T) {
		assert.Equal(t, []string{"invoice", "receipt"},
			removeBlackListedTags(tags, []string{"Paperless-GPT-Failed"}))
	})

	t.Run("removing everything yields an empty list", func(t *testing.T) {
		assert.Empty(t, removeBlackListedTags(tags, []string{"invoice", "paperless-gpt-failed", "receipt"}))
	})
}
