package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	_ "image/jpeg"

	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
)

// getSuggestedCorrespondent generates a suggested correspondent for a document using the LLM
func (app *App) getSuggestedCorrespondent(ctx context.Context, content string, suggestedTitle string, availableCorrespondents []string, correspondentBlackList []string) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":                likelyLanguage,
		"AvailableCorrespondents": availableCorrespondents,
		"BlackList":               correspondentBlackList,
		"Title":                   suggestedTitle,
	}

	availableTokens, err := getAvailableTokensForContent(correspondentTemplate, templateData)
	if err != nil {
		return "", fmt.Errorf("error calculating available tokens: %v", err)
	}

	// Truncate content if needed
	truncatedContent, err := truncateContentByTokens(content, availableTokens)
	if err != nil {
		return "", fmt.Errorf("error truncating content: %v", err)
	}

	// Execute template with truncated content
	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = correspondentTemplate.Execute(&promptBuffer, templateData)
	if err != nil {
		return "", fmt.Errorf("error executing correspondent template: %v", err)
	}

	prompt := promptBuffer.String()
	log.Debugf("Correspondent suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return "", fmt.Errorf("error getting response from LLM: %v", err)
	}

	response := stripReasoning(strings.TrimSpace(completion.Choices[0].Content))
	return response, nil
}

// getSuggestedTags generates suggested tags for a document using the LLM
func (app *App) getSuggestedTags(
	ctx context.Context,
	content string,
	suggestedTitle string,
	availableTags []string,
	originalTags []string,
	logger *logrus.Entry) ([]string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Remove all paperless-gpt related tags from available tags
	availableTags = removeTagFromList(availableTags, manualTag)
	availableTags = removeTagFromList(availableTags, autoTag)
	availableTags = removeTagFromList(availableTags, autoOcrTag)

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":      likelyLanguage,
		"AvailableTags": availableTags,
		"OriginalTags":  originalTags,
		"Title":         suggestedTitle,
	}

	availableTokens, err := getAvailableTokensForContent(tagTemplate, templateData)
	if err != nil {
		logger.Errorf("Error calculating available tokens: %v", err)
		return nil, fmt.Errorf("error calculating available tokens: %v", err)
	}

	// Truncate content if needed
	truncatedContent, err := truncateContentByTokens(content, availableTokens)
	if err != nil {
		logger.Errorf("Error truncating content: %v", err)
		return nil, fmt.Errorf("error truncating content: %v", err)
	}

	// Execute template with truncated content
	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = tagTemplate.Execute(&promptBuffer, templateData)
	if err != nil {
		logger.Errorf("Error executing tag template: %v", err)
		return nil, fmt.Errorf("error executing tag template: %v", err)
	}

	prompt := promptBuffer.String()
	logger.Debugf("Tag suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		logger.Errorf("Error getting response from LLM: %v", err)
		return nil, fmt.Errorf("error getting response from LLM: %v", err)
	}

	response := stripReasoning(completion.Choices[0].Content)

	suggestedTags := strings.Split(response, ",")
	for i, tag := range suggestedTags {
		suggestedTags[i] = strings.TrimSpace(tag)
	}

	// append the original tags to the suggested tags
	suggestedTags = append(suggestedTags, originalTags...)
	// Remove duplicates
	slices.Sort(suggestedTags)
	suggestedTags = slices.Compact(suggestedTags)

	// Filter out tags that are not in the available tags list
	filteredTags := []string{}
	for _, tag := range suggestedTags {
		for _, availableTag := range availableTags {
			if strings.EqualFold(tag, availableTag) {
				filteredTags = append(filteredTags, availableTag)
				break
			}
		}
	}

	return filteredTags, nil
}

// getSuggestedTitle generates a suggested title for a document using the LLM
func (app *App) getSuggestedTitle(ctx context.Context, content string, originalTitle string, logger *logrus.Entry) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language": likelyLanguage,
		"Content":  content,
		"Title":    originalTitle,
	}

	availableTokens, err := getAvailableTokensForContent(titleTemplate, templateData)
	if err != nil {
		logger.Errorf("Error calculating available tokens: %v", err)
		return "", fmt.Errorf("error calculating available tokens: %v", err)
	}

	// Truncate content if needed
	truncatedContent, err := truncateContentByTokens(content, availableTokens)
	if err != nil {
		logger.Errorf("Error truncating content: %v", err)
		return "", fmt.Errorf("error truncating content: %v", err)
	}

	// Execute template with truncated content
	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = titleTemplate.Execute(&promptBuffer, templateData)

	if err != nil {
		return "", fmt.Errorf("error executing title template: %v", err)
	}

	prompt := promptBuffer.String()
	logger.Debugf("Title suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return "", fmt.Errorf("error getting response from LLM: %v", err)
	}
	result := stripReasoning(completion.Choices[0].Content)
	return strings.TrimSpace(strings.Trim(result, "\"")), nil
}

// getSuggestedCreatedDate generates a suggested createdDate for a document using the LLM
func (app *App) getSuggestedCreatedDate(ctx context.Context, content string, logger *logrus.Entry) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language": likelyLanguage,
		"Content":  content,
		"Today":    getTodayDate(), // must be in YYYY-MM-DD format
	}

	availableTokens, err := getAvailableTokensForContent(createdDateTemplate, templateData)
	if err != nil {
		logger.Errorf("Error calculating available tokens: %v", err)
		return "", fmt.Errorf("error calculating available tokens: %v", err)
	}

	// Truncate content if needed
	truncatedContent, err := truncateContentByTokens(content, availableTokens)
	if err != nil {
		logger.Errorf("Error truncating content: %v", err)
		return "", fmt.Errorf("error truncating content: %v", err)
	}

	// Execute template with truncated content
	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = createdDateTemplate.Execute(&promptBuffer, templateData)

	if err != nil {
		return "", fmt.Errorf("error executing createdDate template: %v", err)
	}

	prompt := promptBuffer.String()
	logger.Debugf("CreatedDate suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Parts: []llms.ContentPart{
				llms.TextContent{
					Text: prompt,
				},
			},
			Role: llms.ChatMessageTypeHuman,
		},
	})
	if err != nil {
		return "", fmt.Errorf("error getting response from LLM: %v", err)
	}
	result := stripReasoning(completion.Choices[0].Content)
	return strings.TrimSpace(strings.Trim(result, "\"")), nil
}

// generateDocumentSuggestions generates suggestions for a set of documents
func (app *App) generateDocumentSuggestions(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, logger *logrus.Entry) ([]DocumentSuggestion, error) {
	// Fetch all available tags from paperless-ngx
	availableTagsMap, err := app.Client.GetAllTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available tags: %v", err)
	}

	// Prepare a list of tag names
	availableTagNames := make([]string, 0, len(availableTagsMap))
	for tagName := range availableTagsMap {
		if tagName == manualTag {
			continue
		}
		availableTagNames = append(availableTagNames, tagName)
	}

	// Prepare a list of document correspodents
	availableCorrespondentsMap, err := app.Client.GetAllCorrespondents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available correspondents: %v", err)
	}

	// Prepare a list of correspondent names
	availableCorrespondentNames := make([]string, 0, len(availableCorrespondentsMap))
	for correspondentName := range availableCorrespondentsMap {
		availableCorrespondentNames = append(availableCorrespondentNames, correspondentName)
	}

	documents := suggestionRequest.Documents
	documentSuggestions := []DocumentSuggestion{}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errorsList := make([]error, 0)

	for i := range documents {
		wg.Add(1)
		go func(doc Document) {
			defer wg.Done()
			documentID := doc.ID
			docLogger := documentLogger(documentID)
			docLogger.Printf("Processing Document ID %d...", documentID)

			content := doc.Content
			suggestedTitle := doc.Title
			var suggestedTags []string
			var suggestedCorrespondent string
			var suggestedCreatedDate string

			if suggestionRequest.GenerateTitles {
				suggestedTitle, err = app.getSuggestedTitle(ctx, content, suggestedTitle, docLogger)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					docLogger.Errorf("Error processing document %d: %v", documentID, err)
					return
				}
			}

			if suggestionRequest.GenerateTags {
				suggestedTags, err = app.getSuggestedTags(ctx, content, suggestedTitle, availableTagNames, doc.Tags, docLogger)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					logger.Errorf("Error generating tags for document %d: %v", documentID, err)
					return
				}
			}

			if suggestionRequest.GenerateCorrespondents {
				suggestedCorrespondent, err = app.getSuggestedCorrespondent(ctx, content, suggestedTitle, availableCorrespondentNames, correspondentBlackList)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Errorf("Error generating correspondents for document %d: %v", documentID, err)
					return
				}
			}

			if suggestionRequest.GenerateCreatedDate {
				suggestedCreatedDate, err = app.getSuggestedCreatedDate(ctx, content, docLogger)
				if err != nil {
					mu.Lock()
					errorsList = append(errorsList, fmt.Errorf("Document %d: %v", documentID, err))
					mu.Unlock()
					log.Errorf("Error generating createdDate for document %d: %v", documentID, err)
					return
				}
			}

			mu.Lock()
			suggestion := DocumentSuggestion{
				ID:               documentID,
				OriginalDocument: doc,
			}
			// Titles
			if suggestionRequest.GenerateTitles {
				docLogger.Printf("Suggested title for document %d: %s", documentID, suggestedTitle)
				suggestion.SuggestedTitle = suggestedTitle
			} else {
				suggestion.SuggestedTitle = doc.Title
			}

			// Tags
			if suggestionRequest.GenerateTags {
				docLogger.Printf("Suggested tags for document %d: %v", documentID, suggestedTags)
				suggestion.SuggestedTags = suggestedTags
			} else {
				suggestion.SuggestedTags = doc.Tags
			}

			// Correspondents
			if suggestionRequest.GenerateCorrespondents {
				log.Printf("Suggested correspondent for document %d: %s", documentID, suggestedCorrespondent)
				suggestion.SuggestedCorrespondent = suggestedCorrespondent
			} else {
				suggestion.SuggestedCorrespondent = ""
			}

			// CreatedDate
			if suggestionRequest.GenerateCreatedDate {
				log.Printf("Suggested createdDate for document %d: %s", documentID, suggestedCreatedDate)
				suggestion.SuggestedCreatedDate = suggestedCreatedDate
			} else {
				suggestion.SuggestedCreatedDate = ""
			}
			// Remove manual tag from the list of suggested tags
			suggestion.RemoveTags = []string{manualTag, autoTag}

			documentSuggestions = append(documentSuggestions, suggestion)
			mu.Unlock()
			docLogger.Printf("Document %d processed successfully.", documentID)
		}(documents[i])
	}

	wg.Wait()

	if len(errorsList) > 0 {
		return nil, errorsList[0] // Return the first error encountered
	}

	return documentSuggestions, nil
}

// getTodayDate returns the current date in YYYY-MM-DD format
func getTodayDate() string {
	return time.Now().Format("2006-01-02")
}

// stripReasoning removes the reasoning from the content indicated by <think> and </think> tags.
func stripReasoning(content string) string {
	// Remove reasoning from the content
	reasoningStart := strings.Index(content, "<think>")
	if reasoningStart != -1 {
		reasoningEnd := strings.Index(content, "</think>")
		if reasoningEnd != -1 {
			content = content[:reasoningStart] + content[reasoningEnd+len("</think>"):]
		}
	}

	// Trim whitespace
	content = strings.TrimSpace(content)
	return content
}
