package main

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/tmc/langchaingo/llms"
)

// getAvailableTokensForContent calculates how many tokens are available for content
// by rendering the template with empty content and counting tokens
func getAvailableTokensForContent(tmpl *template.Template, data map[string]interface{}) (int, error) {
	if tokenLimit <= 0 {
		return -1, nil // No limit when disabled
	}

	// Create a copy of data and set "Content" to empty
	templateData := make(map[string]interface{})
	for k, v := range data {
		templateData[k] = v
	}
	templateData["Content"] = ""

	// Execute template with empty content
	var promptBuffer bytes.Buffer
	if err := tmpl.Execute(&promptBuffer, templateData); err != nil {
		return 0, fmt.Errorf("error executing template: %v", err)
	}

	// Count tokens in prompt template
	promptTokens, err := getTokenCount(promptBuffer.String())
	if err != nil {
		return 0, fmt.Errorf("error counting tokens in prompt: %v", err)
	}
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

func getTokenCount(content string) (int, error) {
	return llms.CountTokens(llmModel, content), nil
}

// truncateContentByTokens truncates the content so that its token count does not exceed availableTokens.
// This implementation uses a binary search on runes to find the longest prefix whose token count is within the limit.
// If availableTokens is 0 or negative, the original content is returned.
func truncateContentByTokens(content string, availableTokens int) (string, error) {
	if availableTokens < 0 || tokenLimit <= 0 {
		return content, nil
	}
	totalTokens, err := getTokenCount(content)
	if err != nil {
		return "", fmt.Errorf("error counting tokens: %v", err)
	}
	if totalTokens <= availableTokens {
		return content, nil
	}

	// Convert content to runes for safe slicing.
	runes := []rune(content)
	low := 0
	high := len(runes)
	validCut := 0

	for low <= high {
		mid := (low + high) / 2
		substr := string(runes[:mid])
		count, err := getTokenCount(substr)
		if err != nil {
			return "", fmt.Errorf("error counting tokens in substring: %v", err)
		}
		if count <= availableTokens {
			validCut = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	truncated := string(runes[:validCut])
	// Final verification
	finalTokens, err := getTokenCount(truncated)
	if err != nil {
		return "", fmt.Errorf("error counting tokens in final truncated content: %v", err)
	}
	if finalTokens > availableTokens {
		return "", fmt.Errorf("truncated content still exceeds the available token limit")
	}
	return truncated, nil
}
