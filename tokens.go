package main

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/textsplitter"
)

// getAvailableTokensForContent calculates how many tokens are available for content
// by rendering the template with empty content and counting tokens
func getAvailableTokensForContent(template *template.Template, data map[string]interface{}) (int, error) {
	if tokenLimit <= 0 {
		return 0, nil // No limit when disabled
	}

	// Create a copy of data and set Content to empty
	templateData := make(map[string]interface{})
	for k, v := range data {
		templateData[k] = v
	}
	templateData["Content"] = ""

	// Execute template with empty content
	var promptBuffer bytes.Buffer
	if err := template.Execute(&promptBuffer, templateData); err != nil {
		return 0, fmt.Errorf("error executing template: %v", err)
	}

	// Count tokens in prompt template
	promptTokens := getTokenCount(promptBuffer.String())
	log.Debugf("Prompt template uses %d tokens", promptTokens)

	// Add safety margin for prompt tokens
	promptTokens += 10

	// Calculate available tokens for content
	availableTokens := tokenLimit - promptTokens
	if availableTokens < 0 {
		return 0, fmt.Errorf("prompt template exceeds token limit")
	}
	return availableTokens, nil
}

func getTokenCount(content string) int {
	return llms.CountTokens(llmModel, content)
}

// truncateContentByTokens truncates the content to fit within the specified token limit
func truncateContentByTokens(content string, availableTokens int) (string, error) {
	if availableTokens <= 0 || tokenLimit <= 0 {
		return content, nil
	}
	tokenCount := getTokenCount(content)
	if tokenCount <= availableTokens {
		return content, nil
	}

	splitter := textsplitter.NewTokenSplitter(
		textsplitter.WithChunkSize(availableTokens),
		textsplitter.WithChunkOverlap(0),
		// textsplitter.WithModelName(llmModel),
	)
	chunks, err := splitter.SplitText(content)
	if err != nil {
		return "", fmt.Errorf("error splitting content: %v", err)
	}

	// return the first chunk
	return chunks[0], nil
}
