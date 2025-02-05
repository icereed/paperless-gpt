package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"text/template"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/textsplitter"
)

// Mock LLM for testing
type mockLLM struct {
	lastPrompt string
}

func (m *mockLLM) CreateEmbedding(_ context.Context, texts []string) ([][]float32, error) {
	return nil, nil
}

func (m *mockLLM) Call(_ context.Context, prompt string, _ ...llms.CallOption) (string, error) {
	m.lastPrompt = prompt
	return "test response", nil
}

func (m *mockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, opts ...llms.CallOption) (*llms.ContentResponse, error) {
	m.lastPrompt = messages[0].Parts[0].(llms.TextContent).Text
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content: "test response",
			},
		},
	}, nil
}

// Mock templates for testing
const (
	testTitleTemplate = `
Language: {{.Language}}
Title: {{.Title}}
Content: {{.Content}}
`
	testTagTemplate = `
Language: {{.Language}}
Tags: {{.AvailableTags}}
Content: {{.Content}}
`
	testCorrespondentTemplate = `
Language: {{.Language}}
Content: {{.Content}}
`
)

func TestPromptTokenLimits(t *testing.T) {
	testLogger := logrus.WithField("test", "test")

	// Initialize test templates
	var err error
	titleTemplate, err = template.New("title").Parse(testTitleTemplate)
	require.NoError(t, err)
	tagTemplate, err = template.New("tag").Parse(testTagTemplate)
	require.NoError(t, err)
	correspondentTemplate, err = template.New("correspondent").Parse(testCorrespondentTemplate)
	require.NoError(t, err)

	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Create a test app with mock LLM
	mockLLM := &mockLLM{}
	app := &App{
		LLM: mockLLM,
	}

	// Set up test template
	testTemplate := template.Must(template.New("test").Parse(`
Language: {{.Language}}
Content: {{.Content}}
`))

	tests := []struct {
		name       string
		tokenLimit int
		content    string
	}{
		{
			name:       "no limit",
			tokenLimit: 0,
			content:    "This is the original content that should not be truncated.",
		},
		{
			name:       "content within limit",
			tokenLimit: 100,
			content:    "Short content",
		},
		{
			name:       "content exceeds limit",
			tokenLimit: 50,
			content:    "This is a much longer content that should definitely be truncated to fit within token limits",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Set token limit for this test
			os.Setenv("TOKEN_LIMIT", fmt.Sprintf("%d", tc.tokenLimit))
			resetTokenLimit()

			// Prepare test data
			data := map[string]interface{}{
				"Language": "English",
			}

			// Calculate available tokens
			availableTokens, err := getAvailableTokensForContent(testTemplate, data)
			require.NoError(t, err)

			// Truncate content if needed
			truncatedContent, err := truncateContentByTokens(tc.content, availableTokens)
			require.NoError(t, err)

			// Test with the app's LLM
			ctx := context.Background()
			_, err = app.getSuggestedTitle(ctx, truncatedContent, "Test Title", testLogger)
			require.NoError(t, err)

			// Verify truncation
			if tc.tokenLimit > 0 {
				// Count tokens in final prompt received by LLM
				splitter := textsplitter.NewTokenSplitter()
				tokens, err := splitter.SplitText(mockLLM.lastPrompt)
				require.NoError(t, err)

				// Verify prompt is within limits
				assert.LessOrEqual(t, len(tokens), tc.tokenLimit,
					"Final prompt should be within token limit")

				if len(tc.content) > len(truncatedContent) {
					// Content was truncated
					t.Logf("Content truncated from %d to %d characters",
						len(tc.content), len(truncatedContent))
				}
			} else {
				// No limit set, content should be unchanged
				assert.Contains(t, mockLLM.lastPrompt, tc.content,
					"Original content should be in prompt when no limit is set")
			}
		})
	}
}

func TestTokenLimitInCorrespondentGeneration(t *testing.T) {
	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Create a test app with mock LLM
	mockLLM := &mockLLM{}
	app := &App{
		LLM: mockLLM,
	}

	// Test content that would exceed reasonable token limits
	longContent := "This is a very long content that would normally exceed token limits. " +
		"It contains multiple sentences and should be truncated appropriately " +
		"based on the token limit that we set."

	// Set a small token limit
	os.Setenv("TOKEN_LIMIT", "50")
	resetTokenLimit()

	// Call getSuggestedCorrespondent
	ctx := context.Background()
	availableCorrespondents := []string{"Test Corp", "Example Inc"}
	correspondentBlackList := []string{"Blocked Corp"}

	_, err := app.getSuggestedCorrespondent(ctx, longContent, "Test Title", availableCorrespondents, correspondentBlackList)
	require.NoError(t, err)

	// Verify the final prompt size
	splitter := textsplitter.NewTokenSplitter()
	tokens, err := splitter.SplitText(mockLLM.lastPrompt)
	require.NoError(t, err)

	// Final prompt should be within token limit
	assert.LessOrEqual(t, len(tokens), 50, "Final prompt should be within token limit")
}

func TestTokenLimitInTagGeneration(t *testing.T) {
	testLogger := logrus.WithField("test", "test")

	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Create a test app with mock LLM
	mockLLM := &mockLLM{}
	app := &App{
		LLM: mockLLM,
	}

	// Test content that would exceed reasonable token limits
	longContent := "This is a very long content that would normally exceed token limits. " +
		"It contains multiple sentences and should be truncated appropriately."

	// Set a small token limit
	os.Setenv("TOKEN_LIMIT", "50")
	resetTokenLimit()

	// Call getSuggestedTags
	ctx := context.Background()
	availableTags := []string{"test", "example"}
	originalTags := []string{"original"}

	_, err := app.getSuggestedTags(ctx, longContent, "Test Title", availableTags, originalTags, testLogger)
	require.NoError(t, err)

	// Verify the final prompt size
	splitter := textsplitter.NewTokenSplitter()
	tokens, err := splitter.SplitText(mockLLM.lastPrompt)
	require.NoError(t, err)

	// Final prompt should be within token limit
	assert.LessOrEqual(t, len(tokens), 50, "Final prompt should be within token limit")
}

func TestTokenLimitInTitleGeneration(t *testing.T) {
	testLogger := logrus.WithField("test", "test")

	// Save current env and restore after test
	originalLimit := os.Getenv("TOKEN_LIMIT")
	defer os.Setenv("TOKEN_LIMIT", originalLimit)

	// Create a test app with mock LLM
	mockLLM := &mockLLM{}
	app := &App{
		LLM: mockLLM,
	}

	// Test content that would exceed reasonable token limits
	longContent := "This is a very long content that would normally exceed token limits. " +
		"It contains multiple sentences and should be truncated appropriately."

	// Set a small token limit
	os.Setenv("TOKEN_LIMIT", "50")
	resetTokenLimit()

	// Call getSuggestedTitle
	ctx := context.Background()

	_, err := app.getSuggestedTitle(ctx, longContent, "Original Title", testLogger)
	require.NoError(t, err)

	// Verify the final prompt size
	splitter := textsplitter.NewTokenSplitter()
	tokens, err := splitter.SplitText(mockLLM.lastPrompt)
	require.NoError(t, err)

	// Final prompt should be within token limit
	assert.LessOrEqual(t, len(tokens), 50, "Final prompt should be within token limit")
}
func TestStripReasoning(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No reasoning tags",
			input:    "This is a test content without reasoning tags.",
			expected: "This is a test content without reasoning tags.",
		},
		{
			name:     "Reasoning tags at the start",
			input:    "<think>Start reasoning</think>\n\nContent      \n\n",
			expected: "Content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := stripReasoning(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
