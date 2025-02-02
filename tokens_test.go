package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/textsplitter"
)

// resetTokenLimit parses TOKEN_LIMIT from environment and sets the tokenLimit variable
func resetTokenLimit() {
	// Reset tokenLimit
	tokenLimit = 0
	// Parse from environment
	if limit := os.Getenv("TOKEN_LIMIT"); limit != "" {
		if parsed, err := strconv.Atoi(limit); err == nil {
			tokenLimit = parsed
		}
	}
}

func TestTokenLimit(t *testing.T) {
	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	tests := []struct {
		name      string
		envValue  string
		wantLimit int
	}{
		{
			name:      "empty value",
			envValue:  "",
			wantLimit: 0,
		},
		{
			name:      "zero value",
			envValue:  "0",
			wantLimit: 0,
		},
		{
			name:      "positive value",
			envValue:  "1000",
			wantLimit: 1000,
		},
		{
			name:      "invalid value",
			envValue:  "not-a-number",
			wantLimit: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set environment variable
			os.Setenv("TOKEN_LIMIT", tc.envValue)

			// Set tokenLimit based on environment
			resetTokenLimit()

			assert.Equal(t, tc.wantLimit, tokenLimit)
		})
	}
}

func TestGetAvailableTokensForContent(t *testing.T) {
	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Test template
	tmpl := template.Must(template.New("test").Parse("Template with {{.Var1}} and {{.Content}}"))

	tests := []struct {
		name      string
		limit     int
		data      map[string]interface{}
		wantCount int
		wantErr   bool
	}{
		{
			name:      "disabled token limit",
			limit:     0,
			data:      map[string]interface{}{"Var1": "test"},
			wantCount: -1,
			wantErr:   false,
		},
		{
			name:  "template exceeds limit",
			limit: 2,
			data: map[string]interface{}{
				"Var1": "test",
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:  "available tokens calculation",
			limit: 100,
			data: map[string]interface{}{
				"Var1": "test",
			},
			wantCount: 85,
			wantErr:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set token limit
			os.Setenv("TOKEN_LIMIT", fmt.Sprintf("%d", tc.limit))
			// Set tokenLimit based on environment
			resetTokenLimit()

			count, err := getAvailableTokensForContent(tmpl, tc.data)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantCount, count)
			}
		})
	}
}

func TestTruncateContentByTokens(t *testing.T) {
	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Set a token limit for testing
	os.Setenv("TOKEN_LIMIT", "100")
	// Set tokenLimit based on environment
	resetTokenLimit()

	tests := []struct {
		name            string
		content         string
		availableTokens int
		wantTruncated   bool
		wantErr         bool
	}{
		{
			name:            "no truncation needed",
			content:         "short content",
			availableTokens: 20,
			wantTruncated:   false,
			wantErr:         false,
		},
		{
			name:            "disabled by token limit",
			content:         "any content",
			availableTokens: -1,
			wantTruncated:   false,
			wantErr:         false,
		},
		{
			name:            "truncation needed",
			content:         "This is a much longer content that will definitely need to be truncated because it exceeds the available tokens",
			availableTokens: 10,
			wantTruncated:   true,
			wantErr:         false,
		},
		{
			name:            "empty content",
			content:         "",
			availableTokens: 10,
			wantTruncated:   false,
			wantErr:         false,
		},
		{
			name:            "exact token count",
			content:         "one two three four five",
			availableTokens: 5,
			wantTruncated:   false,
			wantErr:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := truncateContentByTokens(tc.content, tc.availableTokens)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.wantTruncated {
				assert.True(t, len(result) < len(tc.content), "Content should be truncated")
			} else {
				assert.Equal(t, tc.content, result, "Content should not be truncated")
			}
		})
	}
}

func TestTokenLimitIntegration(t *testing.T) {
	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Create a test template
	tmpl := template.Must(template.New("test").Parse(`
Template with variables:
Language: {{.Language}}
Title: {{.Title}}
Content: {{.Content}}
`))

	// Test data
	data := map[string]interface{}{
		"Language": "English",
		"Title":    "Test Document",
	}

	// Test with different token limits
	tests := []struct {
		name      string
		limit     int
		content   string
		wantSize  int
		wantError bool
	}{
		{
			name:      "no limit",
			limit:     0,
			content:   "original content",
			wantSize:  len("original content"),
			wantError: false,
		},
		{
			name:      "sufficient limit",
			limit:     1000,
			content:   "original content",
			wantSize:  len("original content"),
			wantError: false,
		},
		{
			name:      "tight limit",
			limit:     50,
			content:   "This is a long content that should be truncated to fit within the token limit",
			wantSize:  50,
			wantError: false,
		},
		{
			name:      "very small limit",
			limit:     3,
			content:   "Content too large for small limit",
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set token limit
			os.Setenv("TOKEN_LIMIT", fmt.Sprintf("%d", tc.limit))
			// Set tokenLimit based on environment
			resetTokenLimit()

			// First get available tokens
			availableTokens, err := getAvailableTokensForContent(tmpl, data)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Then truncate content
			truncated, err := truncateContentByTokens(tc.content, availableTokens)
			require.NoError(t, err)

			// Finally execute template with truncated content
			data["Content"] = truncated
			var result string
			{
				var buf bytes.Buffer
				err = tmpl.Execute(&buf, data)
				require.NoError(t, err)
				result = buf.String()
			}

			// Verify final size is within limit if limit is enabled
			if tc.limit > 0 {
				splitter := textsplitter.NewTokenSplitter()
				tokens, err := splitter.SplitText(result)
				require.NoError(t, err)
				assert.LessOrEqual(t, len(tokens), tc.limit)
			}
		})
	}
}
