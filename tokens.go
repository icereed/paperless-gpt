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

// truncateContentByTokens truncates the content to fit within the specified token limit
func truncateContentByTokens(content string, availableTokens int) (string, error) {
	if availableTokens <= 0 || tokenLimit <= 0 {
		return content, nil
	}
	tokenCount, err := getTokenCount(content)
	if err != nil {
		return "", fmt.Errorf("error counting tokens: %v", err)
	}
	if tokenCount <= availableTokens {
		return content, nil
	}

	splitter := textsplitter.NewTokenSplitter(
		textsplitter.WithChunkSize(availableTokens),
		textsplitter.WithChunkOverlap(0),
		textsplitter.WithModelName(llmModel),
	)
	chunks, err := splitter.SplitText(content)
	if err != nil {
		return "", fmt.Errorf("error splitting content: %v", err)
	}

	// Validate first chunk's token count
	firstChunk := chunks[0]
	chunkTokens, err := getTokenCount(firstChunk)
	if err != nil {
		return "", fmt.Errorf("error counting tokens in chunk: %v", err)
	}
	if chunkTokens > availableTokens {
		return "", fmt.Errorf("first chunk uses %d tokens which exceeds the limit of %d tokens", chunkTokens, availableTokens)
	}

	// return the first chunk
	return firstChunk, nil
}
