package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
		"CreateNewTags": createNewTags,
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

	// Filter out tags that are not in the available tags list (unless CREATE_NEW_TAGS is enabled)
	if createNewTags {
		// When creating new tags is enabled, keep all non-empty suggested tags
		filteredTags := []string{}
		for _, tag := range suggestedTags {
			if tag != "" {
				// Use the available tag's casing if it exists
				matched := false
				for _, availableTag := range availableTags {
					if strings.EqualFold(tag, availableTag) {
						filteredTags = append(filteredTags, availableTag)
						matched = true
						break
					}
				}
				if !matched {
					filteredTags = append(filteredTags, tag)
				}
			}
		}
		return filteredTags, nil
	}

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

// getSuggestedDocumentType generates a suggested document type for a document using the LLM
func (app *App) getSuggestedDocumentType(
	ctx context.Context,
	content string,
	suggestedTitle string,
	availableDocumentTypes []string,
	logger *logrus.Entry) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":               likelyLanguage,
		"AvailableDocumentTypes": availableDocumentTypes,
		"Title":                  suggestedTitle,
	}

	availableTokens, err := getAvailableTokensForContent(documentTypeTemplate, templateData)
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
	err = documentTypeTemplate.Execute(&promptBuffer, templateData)
	if err != nil {
		logger.Errorf("Error executing document type template: %v", err)
		return "", fmt.Errorf("error executing document type template: %v", err)
	}

	prompt := promptBuffer.String()
	logger.Debugf("Document type suggestion prompt: %s", prompt)

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
		return "", fmt.Errorf("error getting response from LLM: %v", err)
	}

	response := strings.TrimSpace(stripReasoning(completion.Choices[0].Content))

	// Validate that the response is in the available document types list
	for _, docType := range availableDocumentTypes {
		if strings.EqualFold(response, docType) {
			return docType, nil // Return the exact name from available types
		}
	}

	// If not found in available types, return empty string
	if response != "" {
		logger.Warnf("LLM suggested document type '%s' not found in available types, ignoring", response)
	}
	return "", nil
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

// getSuggestedCustomFields generates suggested custom fields for a document using the LLM
func (app *App) getSuggestedCustomFields(ctx context.Context, doc Document, selectedFieldIDs []int, logger *logrus.Entry) ([]CustomFieldSuggestion, error) {
	// Fetch all available custom fields
	allCustomFields, err := app.Client.GetCustomFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching all custom fields: %v", err)
	}

	// Filter to get only the selected custom fields
	var selectedCustomFields []CustomField
	for _, field := range allCustomFields {
		for _, selectedID := range selectedFieldIDs {
			if field.ID == selectedID {
				selectedCustomFields = append(selectedCustomFields, field)
				break
			}
		}
	}

	if len(selectedCustomFields) == 0 {
		return nil, nil // No fields to process
	}

	// Generate XML for the prompt
	var xmlBuilder strings.Builder
	xmlBuilder.WriteString("<custom_fields>\n")
	for _, field := range selectedCustomFields {
		xmlBuilder.WriteString(fmt.Sprintf("  <field name=\"%s\" type=\"%s\"></field>\n", field.Name, field.DataType))
	}
	xmlBuilder.WriteString("</custom_fields>")
	customFieldsXML := xmlBuilder.String()

	templateMutex.RLock()
	defer templateMutex.RUnlock()

	templateData := map[string]interface{}{
		"Language":        getLikelyLanguage(),
		"Title":           doc.Title,
		"CreatedDate":     doc.CreatedDate,
		"DocumentType":    doc.DocumentTypeName,
		"CustomFieldsXML": customFieldsXML,
	}

	availableTokens, err := getAvailableTokensForContent(customFieldTemplate, templateData)
	if err != nil {
		return nil, fmt.Errorf("error calculating available tokens for custom fields: %v", err)
	}

	truncatedContent, err := truncateContentByTokens(doc.Content, availableTokens)
	if err != nil {
		return nil, fmt.Errorf("error truncating content for custom fields: %v", err)
	}

	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = customFieldTemplate.Execute(&promptBuffer, templateData)
	if err != nil {
		return nil, fmt.Errorf("error executing custom field template: %v", err)
	}

	prompt := promptBuffer.String()
	logger.Debugf("Custom field suggestion prompt: %s", prompt)

	completion, err := app.LLM.GenerateContent(ctx, []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: prompt},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error getting response from LLM for custom fields: %v", err)
	}

	response := stripReasoning(completion.Choices[0].Content)
	response = stripMarkdown(response)
	logger.Debugf("LLM response for custom fields: %s", response)

	// Temporary struct to unmarshal LLM response with field name
	type LLMCustomFieldResponse struct {
		Field string      `json:"field"`
		Value interface{} `json:"value"`
	}

	var llmSuggestedFields []LLMCustomFieldResponse
	// Handle empty or non-JSON response gracefully
	if strings.TrimSpace(response) == "" || !strings.HasPrefix(strings.TrimSpace(response), "[") {
		return []CustomFieldSuggestion{}, nil
	}

	err = json.Unmarshal([]byte(response), &llmSuggestedFields)
	if err != nil {
		logger.Errorf("Error unmarshalling custom fields JSON from LLM response: %v. Response: %s", err, response)
		return []CustomFieldSuggestion{}, nil // Return empty slice on parsing error
	}

	// Map field names back to IDs
	fieldNameIdMap := make(map[string]int)
	for _, field := range allCustomFields {
		fieldNameIdMap[field.Name] = field.ID
	}

	var finalSuggestedFields []CustomFieldSuggestion
	for _, llmField := range llmSuggestedFields {
		if id, ok := fieldNameIdMap[llmField.Field]; ok {
			finalSuggestedFields = append(finalSuggestedFields, CustomFieldSuggestion{
				ID:    id,
				Name:  llmField.Field,
				Value: llmField.Value,
			})
		} else {
			logger.Warnf("LLM returned unknown custom field name '%s', skipping.", llmField.Field)
		}
	}

	return finalSuggestedFields, nil
}

type suggestionGenerationContext struct {
	availableTagNames           []string
	availableCorrespondentNames []string
	availableDocumentTypeNames  []string
}

// generateDocumentSuggestions generates suggestions for a set of documents.
func (app *App) generateDocumentSuggestions(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, logger *logrus.Entry) ([]DocumentSuggestion, error) {
	return app.generateDocumentSuggestionsSequential(ctx, suggestionRequest, "", logger)
}

func (app *App) generateDocumentSuggestionsSequential(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, jobID string, logger *logrus.Entry) ([]DocumentSuggestion, error) {
	generationContext, err := app.prepareSuggestionGenerationContext(ctx, suggestionRequest)
	if err != nil {
		return nil, err
	}

	documentSuggestions := make([]DocumentSuggestion, 0, len(suggestionRequest.Documents))
	for index, doc := range suggestionRequest.Documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if jobID != "" {
			suggestionJobStore.updateProgress(jobID, index, doc.ID)
		}

		suggestion, err := app.generateSingleDocumentSuggestion(ctx, suggestionRequest, doc, generationContext, logger)
		if err != nil {
			return nil, err
		}

		documentSuggestions = append(documentSuggestions, suggestion)
		if jobID != "" {
			suggestionJobStore.updateProgress(jobID, index+1, 0)
		}
	}

	return documentSuggestions, nil
}

func (app *App) prepareSuggestionGenerationContext(ctx context.Context, suggestionRequest GenerateSuggestionsRequest) (suggestionGenerationContext, error) {
	generationContext := suggestionGenerationContext{}

	if suggestionRequest.GenerateTags {
		availableTagsMap, err := app.Client.GetAllTags(ctx)
		if err != nil {
			return suggestionGenerationContext{}, fmt.Errorf("failed to fetch available tags: %v", err)
		}

		generationContext.availableTagNames = make([]string, 0, len(availableTagsMap))
		for tagName := range availableTagsMap {
			if tagName == manualTag {
				continue
			}
			generationContext.availableTagNames = append(generationContext.availableTagNames, tagName)
		}
	}

	if suggestionRequest.GenerateCorrespondents {
		availableCorrespondentsMap, err := app.Client.GetAllCorrespondents(ctx)
		if err != nil {
			return suggestionGenerationContext{}, fmt.Errorf("failed to fetch available correspondents: %v", err)
		}

		generationContext.availableCorrespondentNames = make([]string, 0, len(availableCorrespondentsMap))
		for correspondentName := range availableCorrespondentsMap {
			generationContext.availableCorrespondentNames = append(generationContext.availableCorrespondentNames, correspondentName)
		}
	}

	if suggestionRequest.GenerateDocumentTypes {
		availableDocumentTypes, err := app.Client.GetAllDocumentTypes(ctx)
		if err != nil {
			return suggestionGenerationContext{}, fmt.Errorf("failed to fetch available document types: %v", err)
		}

		generationContext.availableDocumentTypeNames = make([]string, 0, len(availableDocumentTypes))
		for _, docType := range availableDocumentTypes {
			generationContext.availableDocumentTypeNames = append(generationContext.availableDocumentTypeNames, docType.Name)
		}
	}

	return generationContext, nil
}

func (app *App) generateSingleDocumentSuggestion(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, doc Document, generationContext suggestionGenerationContext, logger *logrus.Entry) (DocumentSuggestion, error) {
	documentID := doc.ID
	docLogger := documentLogger(documentID)
	startTime := time.Now()
	docLogger.Printf("Processing Document ID %d...", documentID)

	content := doc.Content
	suggestedTitle := doc.Title
	var suggestedTags []string
	var suggestedCorrespondent string
	var suggestedDocumentType string
	var suggestedCreatedDate string
	var suggestedCustomFields []CustomFieldSuggestion

	if suggestionRequest.GenerateTitles {
		var err error
		suggestedTitle, err = app.getSuggestedTitle(ctx, content, suggestedTitle, docLogger)
		if err != nil {
			docLogger.Errorf("Error processing document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateTags {
		var err error
		suggestedTags, err = app.getSuggestedTags(ctx, content, suggestedTitle, generationContext.availableTagNames, doc.Tags, docLogger)
		if err != nil {
			logger.Errorf("Error generating tags for document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateCorrespondents {
		var err error
		suggestedCorrespondent, err = app.getSuggestedCorrespondent(ctx, content, suggestedTitle, generationContext.availableCorrespondentNames, correspondentBlackList)
		if err != nil {
			log.Errorf("Error generating correspondents for document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateDocumentTypes {
		if len(generationContext.availableDocumentTypeNames) == 0 {
			docLogger.Debug("Document type generation is enabled, but no document types are available in paperless-ngx.")
		} else {
			var err error
			suggestedDocumentType, err = app.getSuggestedDocumentType(ctx, content, suggestedTitle, generationContext.availableDocumentTypeNames, docLogger)
			if err != nil {
				log.Errorf("Error generating document type for document %d: %v", documentID, err)
				return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
			}
		}
	}

	if suggestionRequest.GenerateCreatedDate {
		var err error
		suggestedCreatedDate, err = app.getSuggestedCreatedDate(ctx, content, docLogger)
		if err != nil {
			log.Errorf("Error generating createdDate for document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateCustomFields {
		settingsMutex.RLock()
		selectedIDs := settings.CustomFieldsSelectedIDs
		settingsMutex.RUnlock()

		if len(selectedIDs) == 0 {
			log.Warnf("Custom field generation is enabled, but no custom fields are selected in the settings. Please select at least one custom field for this feature to work.")
		} else {
			var err error
			suggestedCustomFields, err = app.getSuggestedCustomFields(ctx, doc, selectedIDs, docLogger)
			if err != nil {
				log.Errorf("Error generating custom fields for document %d: %v", documentID, err)
				return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
			}
		}
	}

	suggestion := DocumentSuggestion{
		ID:               documentID,
		OriginalDocument: doc,
	}
	settingsMutex.RLock()
	suggestion.CustomFieldsWriteMode = settings.CustomFieldsWriteMode
	suggestion.CustomFieldsEnable = settings.CustomFieldsEnable
	settingsMutex.RUnlock()

	if suggestionRequest.GenerateTitles {
		docLogger.Printf("Suggested title for document %d: %s", documentID, suggestedTitle)
		suggestion.SuggestedTitle = suggestedTitle
	} else {
		suggestion.SuggestedTitle = doc.Title
	}

	if suggestionRequest.GenerateTags {
		docLogger.Printf("Suggested tags for document %d: %v", documentID, suggestedTags)
		suggestion.SuggestedTags = suggestedTags
	} else {
		suggestion.SuggestedTags = doc.Tags
	}

	if suggestionRequest.GenerateCorrespondents {
		log.Printf("Suggested correspondent for document %d: %s", documentID, suggestedCorrespondent)
		suggestion.SuggestedCorrespondent = suggestedCorrespondent
	}

	if suggestionRequest.GenerateDocumentTypes {
		log.Printf("Suggested document type for document %d: %s", documentID, suggestedDocumentType)
		suggestion.SuggestedDocumentType = suggestedDocumentType
	}

	if suggestionRequest.GenerateCreatedDate {
		log.Printf("Suggested createdDate for document %d: %s", documentID, suggestedCreatedDate)
		suggestion.SuggestedCreatedDate = suggestedCreatedDate
	}

	if suggestionRequest.GenerateCustomFields {
		log.Printf("Suggested custom fields for document %d: %v", documentID, suggestedCustomFields)
		suggestion.SuggestedCustomFields = suggestedCustomFields
	}

	suggestion.RemoveTags = []string{manualTag, autoTag}

	elapsed := time.Since(startTime)
	runtime := time.Unix(0, elapsed.Nanoseconds()).UTC()
	docLogger.Printf("Document %d processed successfully. Runtime: %s", documentID, runtime.Format("15:04:05"))

	return suggestion, nil
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

// stripMarkdown removes the markdown code block from the content.
func stripMarkdown(content string) string {
	// Remove markdown code block
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
	}
	return strings.TrimSpace(content)
}
