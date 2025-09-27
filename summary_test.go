package main

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSuggestedSummary(t *testing.T) {
	// Set up mock LLM
	mockLLM := &mockLLM{
		Response: "This document contains financial information for Q3 2023 quarterly report with revenue and expense details.",
	}
	
	app := &App{
		LLM: mockLLM,
	}

	// Load templates for testing
	err := loadTemplates()
	require.NoError(t, err)

	logger := logrus.NewEntry(logrus.New())
	ctx := context.Background()

	testContent := "This is a test document about financial quarterly report for Q3 2023. It contains revenue information and expenses breakdown."
	testTitle := "Q3 2023 Financial Report"

	summary, err := app.getSuggestedSummary(ctx, testContent, testTitle, logger)

	require.NoError(t, err)
	assert.NotEmpty(t, summary)
	assert.Equal(t, "This document contains financial information for Q3 2023 quarterly report with revenue and expense details.", summary)
}

func TestGenerateDocumentSuggestionsWithSummary(t *testing.T) {
	// Set up mock LLM
	mockLLM := &mockLLM{
		Response: "This is a summary of the test document about financial information.",
	}
	
	// Mock client with proper tag/correspondent returns
	mockClient := &mockPaperlessClient{}
	
	app := &App{
		LLM:    mockLLM,
		Client: mockClient,
	}

	// Load templates for testing
	err := loadTemplates()
	require.NoError(t, err)

	// Test document
	documents := []Document{
		{
			ID:      1,
			Title:   "Test Document",
			Content: "This is a test document about financial information.",
			Tags:    []string{"finance"},
		},
	}

	// Request with summary generation enabled
	request := GenerateSuggestionsRequest{
		Documents:       documents,
		GenerateSummary: true,
	}

	suggestions, err := app.generateDocumentSuggestions(context.Background(), request, logrus.NewEntry(logrus.New()))

	require.NoError(t, err)
	require.Len(t, suggestions, 1)
	assert.NotEmpty(t, suggestions[0].SuggestedSummary)
	assert.Equal(t, "This is a summary of the test document about financial information.", suggestions[0].SuggestedSummary)
}