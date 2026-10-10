package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"paperless-gpt/internal/textsanitize"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	_ "image/jpeg"

	"paperless-gpt/sanitize"

	"github.com/sirupsen/logrus"
	"github.com/tmc/langchaingo/llms"
)

// getSuggestedCorrespondent generates a suggested correspondent for a document using the LLM.
// tmplOverride, when non-nil, replaces the global correspondentTemplate for this call.
func (app *App) getSuggestedCorrespondent(ctx context.Context, content string, suggestedTitle string, availableCorrespondents []string, correspondentBlackList []string, tmplOverride *template.Template) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	activeTmpl := correspondentTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":                likelyLanguage,
		"AvailableCorrespondents": availableCorrespondents,
		"BlackList":               correspondentBlackList,
		"Title":                   suggestedTitle,
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
	if correspondentPromptTokenLimit > 0 {
		availableTokens, err = getAvailableTokensForContentWithLimit(activeTmpl, templateData, correspondentPromptTokenLimit)
	}
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
	err = activeTmpl.Execute(&promptBuffer, templateData)
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

	response := textsanitize.StripReasoning(strings.TrimSpace(completion.Choices[0].Content))
	return response, nil
}

// getSuggestedTags generates suggested tags for a document using the LLM.
// tmplOverride, when non-nil, replaces the global tagTemplate for this call.
func (app *App) getSuggestedTags(
	ctx context.Context,
	content string,
	suggestedTitle string,
	availableTags []string,
	originalTags []string,
	logger *logrus.Entry,
	tmplOverride *template.Template) ([]string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	activeTmpl := tagTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	// Remove all paperless-gpt related tags from available tags
	availableTags = removeSystemTags(availableTags)

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":      likelyLanguage,
		"AvailableTags": availableTags,
		"OriginalTags":  originalTags,
		"Title":         suggestedTitle,
		"CreateNewTags": createNewTags,
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
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
	err = activeTmpl.Execute(&promptBuffer, templateData)
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

	response := textsanitize.StripReasoning(completion.Choices[0].Content)

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
		// The original tags were merged in above, and on a document being
		// processed those include the trigger tag paperless-gpt is reacting to.
		// With CREATE_NEW_TAGS on, nothing else here would drop them, so a
		// system tag would come back out as a "suggestion" and be re-applied.
		return removeSystemTags(filteredTags), nil
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

	// Belt and braces: availableTags is already system-tag-free, so this only
	// matters if that ever regresses. paperless-gpt applies its own tags
	// through AddTags/RemoveTags, never through a suggestion.
	return removeSystemTags(filteredTags), nil
}

// getSuggestedDocumentType generates a suggested document type for a document using the LLM.
// tmplOverride, when non-nil, replaces the global documentTypeTemplate for this call.
func (app *App) getSuggestedDocumentType(
	ctx context.Context,
	content string,
	suggestedTitle string,
	availableDocumentTypes []string,
	logger *logrus.Entry,
	tmplOverride *template.Template) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	activeTmpl := documentTypeTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language":               likelyLanguage,
		"AvailableDocumentTypes": availableDocumentTypes,
		"Title":                  suggestedTitle,
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
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
	err = activeTmpl.Execute(&promptBuffer, templateData)
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

	response := strings.TrimSpace(textsanitize.StripReasoning(completion.Choices[0].Content))

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

// getSuggestedTitle generates a suggested title for a document using the LLM.
// tmplOverride, when non-nil, replaces the global titleTemplate for this call.
func (app *App) getSuggestedTitle(ctx context.Context, content string, originalTitle string, logger *logrus.Entry, tmplOverride *template.Template) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	activeTmpl := titleTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language": likelyLanguage,
		"Content":  content,
		"Title":    originalTitle,
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
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
	err = activeTmpl.Execute(&promptBuffer, templateData)

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
	result := textsanitize.StripReasoning(completion.Choices[0].Content)
	return strings.TrimSpace(strings.Trim(result, "\"")), nil
}

// getSuggestedCreatedDate generates a suggested createdDate for a document using the LLM.
// tmplOverride, when non-nil, replaces the global createdDateTemplate for this call.
func (app *App) getSuggestedCreatedDate(ctx context.Context, content string, logger *logrus.Entry, tmplOverride *template.Template) (string, error) {
	likelyLanguage := getLikelyLanguage()

	templateMutex.RLock()
	activeTmpl := createdDateTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	// Get available tokens for content
	templateData := map[string]interface{}{
		"Language": likelyLanguage,
		"Content":  content,
		"Today":    getTodayDate(), // must be in YYYY-MM-DD format
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
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
	err = activeTmpl.Execute(&promptBuffer, templateData)

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
	result := textsanitize.StripReasoning(completion.Choices[0].Content)
	result = strings.TrimSpace(strings.Trim(result, "\""))
	// paperless-ngx only accepts YYYY-MM-DD. Models often emit the same
	// date with dots or slashes, which then fails validation and is dropped.
	return normalizeCreatedDate(result), nil
}

var xmlAttrEscaper = strings.NewReplacer(
	"&", "&amp;",
	`"`, "&quot;",
	"'", "&apos;",
	"<", "&lt;",
	">", "&gt;",
)

var xmlTextEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

func escapeXMLAttr(s string) string { return xmlAttrEscaper.Replace(s) }
func escapeXMLText(s string) string { return xmlTextEscaper.Replace(s) }

// A paperless-ngx "documentlink" custom field holds a list of document ids,
// which the LLM cannot know. Instead it is asked for the identifiers of the
// documents this one refers to (invoice number, contract number, ...), and
// resolveDocumentLinks looks those up in paperless-ngx.
const (
	documentLinkFieldType = "documentlink"

	// documentLinkFieldHint is sent inside the field's XML element rather than
	// in the prompt template, so it also reaches users who keep a customized
	// custom_field_prompt.tmpl from an older release.
	documentLinkFieldHint = "Links to other documents that this document refers to. Return a JSON array of strings " +
		"with the identifiers of those documents exactly as printed in this document, e.g. an invoice, contract, " +
		"order or case number. Only include identifiers that belong to another document, not personal data like " +
		"names, addresses, IBANs or phone numbers."

	// maxDocumentLinkMatches is how many other documents a single reference may
	// match before it is considered too generic to link, e.g. a customer
	// number printed on every letter from the same company.
	maxDocumentLinkMatches = 3

	// minDocumentLinkReferenceLength keeps short fragments like "12" from
	// matching all over the archive.
	minDocumentLinkReferenceLength = 4
)

// getSuggestedCustomFields generates suggested custom fields for a document using the LLM.
// tmplOverride, when non-nil, replaces the global customFieldTemplate for this call.
func (app *App) getSuggestedCustomFields(ctx context.Context, doc Document, selectedFieldIDs []int, logger *logrus.Entry, tmplOverride *template.Template) ([]CustomFieldSuggestion, error) {
	// Fetch all available custom fields
	allCustomFields, err := app.Client.GetCustomFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching all custom fields: %v", err)
	}

	// Filter to get only the selected custom fields
	var selectedCustomFields []CustomField
	for _, field := range allCustomFields {
		if slices.Contains(selectedFieldIDs, field.ID) {
			selectedCustomFields = append(selectedCustomFields, field)
		}
	}

	if len(selectedCustomFields) == 0 {
		return nil, nil // No fields to process
	}

	// Generate XML for the prompt
	var xmlBuilder strings.Builder
	xmlBuilder.WriteString("<custom_fields>\n")
	for _, field := range selectedCustomFields {
		if field.DataType == "select" && field.ExtraData != nil && len(field.ExtraData.SelectOptions) > 0 {
			xmlBuilder.WriteString(fmt.Sprintf("  <field name=\"%s\" type=\"%s\">\n", escapeXMLAttr(field.Name), escapeXMLAttr(field.DataType)))
			for _, opt := range field.ExtraData.SelectOptions {
				xmlBuilder.WriteString(fmt.Sprintf("    <option id=\"%s\">%s</option>\n", escapeXMLAttr(opt.ID), escapeXMLText(opt.Label)))
			}
			xmlBuilder.WriteString("  </field>\n")
		} else if field.DataType == documentLinkFieldType {
			xmlBuilder.WriteString(fmt.Sprintf("  <field name=\"%s\" type=\"%s\">\n", escapeXMLAttr(field.Name), escapeXMLAttr(field.DataType)))
			xmlBuilder.WriteString(fmt.Sprintf("    <description>%s</description>\n", escapeXMLText(documentLinkFieldHint)))
			xmlBuilder.WriteString("  </field>\n")
		} else {
			xmlBuilder.WriteString(fmt.Sprintf("  <field name=\"%s\" type=\"%s\"></field>\n", escapeXMLAttr(field.Name), escapeXMLAttr(field.DataType)))
		}
	}
	xmlBuilder.WriteString("</custom_fields>")
	customFieldsXML := xmlBuilder.String()

	templateMutex.RLock()
	activeTmpl := customFieldTemplate
	templateMutex.RUnlock()
	if tmplOverride != nil {
		activeTmpl = tmplOverride
	}

	templateData := map[string]interface{}{
		"Language":        getLikelyLanguage(),
		"Title":           doc.Title,
		"CreatedDate":     doc.CreatedDate,
		"DocumentType":    doc.DocumentTypeName,
		"CustomFieldsXML": customFieldsXML,
	}

	availableTokens, err := getAvailableTokensForContent(activeTmpl, templateData)
	if err != nil {
		return nil, fmt.Errorf("error calculating available tokens for custom fields: %v", err)
	}

	truncatedContent, err := truncateContentByTokens(sanitize.Sanitize(doc.Content), availableTokens)
	if err != nil {
		return nil, fmt.Errorf("error truncating content for custom fields: %v", err)
	}

	var promptBuffer bytes.Buffer
	templateData["Content"] = truncatedContent
	err = activeTmpl.Execute(&promptBuffer, templateData)
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

	response := textsanitize.StripReasoning(completion.Choices[0].Content)
	logger.Debugf("LLM response for custom fields: %s", response)

	knownNames := make([]string, 0, len(selectedCustomFields))
	for _, field := range selectedCustomFields {
		knownNames = append(knownNames, field.Name)
	}
	llmSuggestedFields, ok := parseCustomFieldLLMResponse(response, knownNames)
	if !ok {
		// A non-empty reply that is not an array, a single {field,value}
		// object, or a map of known field names used to be discarded with
		// no log above debug, and the document was then marked processed.
		logger.Warnf("Custom field response is not usable JSON, skipping custom fields. Starts with: %s", truncateForLog(response, 80))
		return []CustomFieldSuggestion{}, nil
	}

	// Map field names back to fields. Only the fields sent in the prompt are
	// accepted, so the LLM cannot fill a field the user did not select.
	fieldsByName := make(map[string]CustomField)
	for _, field := range selectedCustomFields {
		fieldsByName[field.Name] = field
	}

	var finalSuggestedFields []CustomFieldSuggestion
	for _, llmField := range llmSuggestedFields {
		field, found := fieldsByName[llmField.Field]
		if !found {
			// In selection order, so fields whose names differ only in case
			// resolve the same way every time.
			for _, candidate := range selectedCustomFields {
				if strings.EqualFold(candidate.Name, llmField.Field) {
					field, found = candidate, true
					break
				}
			}
		}
		if !found {
			logger.Warnf("LLM returned custom field '%s', which was not requested, skipping.", llmField.Field)
			continue
		}
		value := llmField.Value
		if field.DataType == documentLinkFieldType {
			linkedIDs := app.resolveDocumentLinks(ctx, doc.ID, value, logger)
			if len(linkedIDs) == 0 {
				logger.Infof("None of the references %v for custom field '%s' matched another document, skipping the field.", value, field.Name)
				continue
			}
			value = linkedIDs
		}
		finalSuggestedFields = append(finalSuggestedFields, CustomFieldSuggestion{
			ID:    field.ID,
			Name:  field.Name,
			Value: value,
		})
	}

	return finalSuggestedFields, nil
}

// resolveDocumentLinks turns the references the LLM extracted for a
// documentlink field into the ids of the documents they identify. A reference
// is only linked if it matches between one and maxDocumentLinkMatches other
// documents; references that match nothing or are too generic are dropped, as
// are lookup errors, so a bad reference never fails the whole document.
func (app *App) resolveDocumentLinks(ctx context.Context, documentID int, value interface{}, logger *logrus.Entry) []int {
	var linkedIDs []int
	for _, reference := range documentLinkReferences(value) {
		if len([]rune(reference)) < minDocumentLinkReferenceLength {
			logger.Debugf("Document link reference %q is too short to look up, skipping.", reference)
			continue
		}

		// One extra result for the document itself and one more to tell
		// whether the reference matches too many documents.
		ids, err := app.Client.FindDocumentIDsByReference(ctx, reference, maxDocumentLinkMatches+2)
		if err != nil {
			logger.Warnf("Looking up document link reference %q failed, skipping it: %v", reference, err)
			continue
		}
		ids = slices.DeleteFunc(ids, func(id int) bool { return id == documentID })

		switch {
		case len(ids) == 0:
			logger.Debugf("Document link reference %q matched no other document.", reference)
		case len(ids) > maxDocumentLinkMatches:
			logger.Debugf("Document link reference %q matched more than %d other documents, too generic to link.", reference, maxDocumentLinkMatches)
		default:
			for _, id := range ids {
				if !slices.Contains(linkedIDs, id) {
					linkedIDs = append(linkedIDs, id)
				}
			}
		}
	}
	return linkedIDs
}

// documentLinkReferences normalizes the LLM's value for a documentlink field
// to a list of reference strings. The prompt asks for an array of strings, but
// a single string or bare numbers are accepted as well.
func documentLinkReferences(value interface{}) []string {
	var values []interface{}
	switch v := value.(type) {
	case []interface{}:
		values = v
	default:
		values = []interface{}{v}
	}

	var references []string
	for _, v := range values {
		var reference string
		switch v := v.(type) {
		case string:
			reference = v
		case float64:
			reference = strconv.FormatFloat(v, 'f', -1, 64)
		}
		if reference = strings.TrimSpace(reference); reference != "" {
			references = append(references, reference)
		}
	}
	return references
}

// suggestionGenerationContext carries the paperless-ngx metadata that suggestion
// generation needs, fetched once per request instead of once per document.
type suggestionGenerationContext struct {
	availableTagNames           []string
	availableCorrespondentNames []string
	availableDocumentTypeNames  []string
}

// prepareSuggestionGenerationContext fetches only the metadata the request actually
// asks for, so e.g. a titles-only run does not depend on the tags endpoint.
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

// generateSingleDocumentSuggestion runs the requested generators for one document.
func (app *App) generateSingleDocumentSuggestion(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, doc Document, generationContext suggestionGenerationContext, logger *logrus.Entry) (DocumentSuggestion, error) {
	documentID := doc.ID
	docLogger := documentLogger(documentID)
	startTime := time.Now()
	docLogger.Printf("Processing Document ID %d...", documentID)

	// Resolve workflow overrides (templates + generation flags).
	var activeWorkflow WorkflowConfig
	hasWorkflow := suggestionRequest.Workflow != nil
	if hasWorkflow {
		activeWorkflow = *suggestionRequest.Workflow
		suggestionRequest = resolveGenerationFlags(suggestionRequest, activeWorkflow)
		docLogger.Infof("Using workflow %q (trigger: %s)", activeWorkflow.Name, activeWorkflow.TriggerTag)
	}

	// Helper to look up a workflow template override (nil when no override or no workflow).
	wfTemplate := func(promptName string, globalTmpl *template.Template) *template.Template {
		if !hasWorkflow {
			return nil
		}
		tmpl, err := getWorkflowTemplate(activeWorkflow, promptName, globalTmpl)
		if err != nil {
			docLogger.Warnf("Workflow template error for %q: %v – falling back to global", promptName, err)
			return nil
		}
		if tmpl == globalTmpl {
			return nil // no override; signal callers to use global
		}
		return tmpl
	}

	content := sanitize.Sanitize(doc.Content)
	suggestedTitle := doc.Title
	var suggestedTags []string
	var suggestedCorrespondent string
	var suggestedDocumentType string
	var suggestedCreatedDate string
	var suggestedCustomFields []CustomFieldSuggestion
	var err error

	if suggestionRequest.GenerateTitles {
		suggestedTitle, err = app.getSuggestedTitle(ctx, content, suggestedTitle, docLogger, wfTemplate("title_prompt", titleTemplate))
		if err != nil {
			docLogger.Errorf("Error processing document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateTags {
		suggestedTags, err = app.getSuggestedTags(ctx, content, suggestedTitle, generationContext.availableTagNames, doc.Tags, docLogger, wfTemplate("tag_prompt", tagTemplate))
		if err != nil {
			logger.Errorf("Error generating tags for document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateCorrespondents {
		promptCorrespondents := filterCorrespondentsForPrompt(generationContext.availableCorrespondentNames, content, suggestedTitle, correspondentPromptLimit)
		suggestedCorrespondent, err = app.getSuggestedCorrespondent(ctx, content, suggestedTitle, promptCorrespondents, correspondentBlackList, wfTemplate("correspondent_prompt", correspondentTemplate))
		if err != nil {
			log.Errorf("Error generating correspondents for document %d: %v", documentID, err)
			return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
		}
	}

	if suggestionRequest.GenerateDocumentTypes {
		if len(generationContext.availableDocumentTypeNames) == 0 {
			docLogger.Debug("Document type generation is enabled, but no document types are available in paperless-ngx.")
		} else {
			suggestedDocumentType, err = app.getSuggestedDocumentType(ctx, content, suggestedTitle, generationContext.availableDocumentTypeNames, docLogger, wfTemplate("document_type_prompt", documentTypeTemplate))
			if err != nil {
				log.Errorf("Error generating document type for document %d: %v", documentID, err)
				return DocumentSuggestion{}, fmt.Errorf("Document %d: %v", documentID, err)
			}
		}
	}

	if suggestionRequest.GenerateCreatedDate {
		suggestedCreatedDate, err = app.getSuggestedCreatedDate(ctx, content, docLogger, wfTemplate("date_prompt", createdDateTemplate))
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
			suggestedCustomFields, err = app.getSuggestedCustomFields(ctx, doc, selectedIDs, docLogger, wfTemplate("custom_field_prompt", customFieldTemplate))
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

	// Document Type
	if suggestionRequest.GenerateDocumentTypes {
		log.Printf("Suggested document type for document %d: %s", documentID, suggestedDocumentType)
		suggestion.SuggestedDocumentType = suggestedDocumentType
	} else {
		suggestion.SuggestedDocumentType = ""
	}

	// CreatedDate
	if suggestionRequest.GenerateCreatedDate {
		log.Printf("Suggested createdDate for document %d: %s", documentID, suggestedCreatedDate)
		suggestion.SuggestedCreatedDate = suggestedCreatedDate
	} else {
		suggestion.SuggestedCreatedDate = ""
	}

	// Custom Fields
	if suggestionRequest.GenerateCustomFields {
		log.Printf("Suggested custom fields for document %d: %v", documentID, suggestedCustomFields)
		suggestion.SuggestedCustomFields = suggestedCustomFields
	}

	suggestion.RemoveTags, suggestion.AddTags = handoverTags(app.autoTagComplete, suggestionRequest.IsAutoProcessing, suggestionRequest.TriggerTag, suggestionRequest.Workflow)
	if len(suggestion.AddTags) > 0 {
		docLogger.Debugf("Adding completion tags %v", suggestion.AddTags)
	}

	elapsed := time.Since(startTime)
	// Format as HH:MM:SS using UTC zero-time base.
	runtime := time.Unix(0, elapsed.Nanoseconds()).UTC()
	docLogger.Printf("Document %d processed successfully. Runtime: %s",
		documentID, runtime.Format("15:04:05"))

	return suggestion, nil
}

// generateDocumentSuggestions generates suggestions for a set of documents in parallel.
// Any document error fails the whole request (legacy synchronous behavior).
func (app *App) generateDocumentSuggestions(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, logger *logrus.Entry) ([]DocumentSuggestion, error) {
	generationContext, err := app.prepareSuggestionGenerationContext(ctx, suggestionRequest)
	if err != nil {
		return nil, err
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
			suggestion, err := app.generateSingleDocumentSuggestion(ctx, suggestionRequest, doc, generationContext, logger)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsList = append(errorsList, err)
				return
			}
			documentSuggestions = append(documentSuggestions, suggestion)
		}(documents[i])
	}

	wg.Wait()

	if len(errorsList) > 0 {
		return nil, errorsList[0] // Return the first error encountered
	}

	return documentSuggestions, nil
}

// generateDocumentSuggestionsForJob processes documents sequentially so an async job can
// report per-document progress. A document that fails is recorded and skipped; a cancelled
// or timed-out context aborts the whole job.
func (app *App) generateDocumentSuggestionsForJob(ctx context.Context, suggestionRequest GenerateSuggestionsRequest, jobID string, logger *logrus.Entry) ([]DocumentSuggestion, []SuggestionJobDocumentFailure, error) {
	generationContext, err := app.prepareSuggestionGenerationContext(ctx, suggestionRequest)
	if err != nil {
		return nil, nil, err
	}

	documentSuggestions := make([]DocumentSuggestion, 0, len(suggestionRequest.Documents))
	failures := []SuggestionJobDocumentFailure{}
	for index, doc := range suggestionRequest.Documents {
		if err := ctx.Err(); err != nil {
			return documentSuggestions, failures, err
		}
		suggestionJobStore.updateProgress(jobID, index, doc.ID)

		suggestion, err := app.generateSingleDocumentSuggestion(ctx, suggestionRequest, doc, generationContext, logger)
		if err != nil {
			if ctx.Err() != nil {
				return documentSuggestions, failures, ctx.Err()
			}
			failures = append(failures, SuggestionJobDocumentFailure{DocumentID: doc.ID, DocumentTitle: doc.Title, Error: err.Error()})
			logger.Errorf("Suggestion job %s: document %d failed: %v", jobID, doc.ID, err)
		} else {
			documentSuggestions = append(documentSuggestions, suggestion)
		}
		suggestionJobStore.updateProgress(jobID, index+1, 0)
	}

	return documentSuggestions, failures, nil
}

// getTodayDate returns the current date in YYYY-MM-DD format
func getTodayDate() string {
	return time.Now().Format("2006-01-02")
}

// llmCustomFieldResponse is one field/value pair from an LLM custom-field reply.
type llmCustomFieldResponse struct {
	Field string      `json:"field"`
	Value interface{} `json:"value"`
}

// parseCustomFieldLLMResponse reads the shapes models actually return for
// custom fields: a JSON array of {field, value}, one such object, or a map
// from field name to value. An empty reply is a successful "nothing found".
// ok is false when the reply is non-empty and none of those shapes match, so
// the caller can say so instead of dropping the fields silently.
//
// knownNames is the fields that were asked for. A name→value map is only
// accepted for those names; other keys are ignored. Names match
// case-insensitively, and the returned Field is the known spelling.
func parseCustomFieldLLMResponse(response string, knownNames []string) (fields []llmCustomFieldResponse, ok bool) {
	cleaned := cleanLLMJSON(response)
	if cleaned == "" {
		return nil, true
	}

	switch cleaned[0] {
	case '[':
		var arr []llmCustomFieldResponse
		if err := json.Unmarshal([]byte(cleaned), &arr); err != nil {
			return nil, false
		}
		return arr, true
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(cleaned), &obj); err != nil {
			return nil, false
		}
		// A single {"field": ..., "value": ...} object. Both keys are
		// required: a map reply for a custom field that is itself named
		// "field" ({"field": "INV-1"}) must be read as a map.
		rawField, hasField := rawMessageByKey(obj, "field")
		_, hasValue := rawMessageByKey(obj, "value")
		if hasField && hasValue {
			var asString string
			if err := json.Unmarshal(rawField, &asString); err == nil && strings.TrimSpace(asString) != "" {
				var one llmCustomFieldResponse
				if err := json.Unmarshal([]byte(cleaned), &one); err == nil && strings.TrimSpace(one.Field) != "" {
					return []llmCustomFieldResponse{one}, true
				}
			}
		}
		var out []llmCustomFieldResponse
		for _, name := range knownNames {
			raw, has := rawMessageByKey(obj, name)
			if !has {
				continue
			}
			var val interface{}
			if err := json.Unmarshal(raw, &val); err != nil || val == nil {
				continue
			}
			out = append(out, llmCustomFieldResponse{Field: name, Value: val})
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

// rawMessageByKey finds key in a JSON object, case-insensitively. An exact
// match wins; otherwise the keys are tried in sorted order, so a reply with
// keys differing only in case always resolves the same way.
func rawMessageByKey(obj map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if raw, ok := obj[key]; ok {
		return raw, true
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if strings.EqualFold(k, key) {
			return obj[k], true
		}
	}
	return nil, false
}

// cleanLLMJSON strips a markdown fence and the whitespace JSON rejects.
// Models indent with U+00A0, and a Latin-1 misread of that byte turns into
// U+00C2 followed by U+00A0. Either one makes encoding/json stop.
func cleanLLMJSON(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "\uFEFF")
	content = strings.NewReplacer(
		"\u00c2\u00a0", " ",
		"\u00a0", " ",
		"\u202f", " ",
		"\u2007", " ",
		"\u200b", "",
		"\ufeff", "",
	).Replace(content)
	return stripMarkdown(content)
}

// truncateForLog shortens a model reply so a warning can quote it without
// dumping the whole document.
func truncateForLog(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// stripMarkdown removes one surrounding markdown code fence, including a
// language tag such as ```json. A fence with no newline (` ```json[{}]``` `)
// is handled too: that is the form the previous trim only accepted.
func stripMarkdown(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	rest := strings.TrimPrefix(content, "```")
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		lang := strings.TrimSpace(rest[:nl])
		if lang == "" || isFenceLanguage(lang) {
			rest = rest[nl+1:]
		}
	} else if len(rest) >= 4 && strings.EqualFold(rest[:4], "json") {
		rest = rest[4:]
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, "```")
	return strings.TrimSpace(rest)
}

func isFenceLanguage(s string) bool {
	if s == "" || len(s) > 16 {
		return false
	}
	for _, r := range s {
		if r != '-' && r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// handoverTags returns the tags to take off a processed document and the
// tags to put on it. The trigger tags always come off: the global ones and,
// for auto-processing, the tag the document was found under. A completion tag
// is only added for auto-processing, not for a manual review: a workflow's
// own completion tag when it has one, AUTO_TAG_COMPLETE otherwise, so a
// workflow can mark its documents differently from the default path.
func handoverTags(autoTagComplete string, isAutoProcessing bool, triggerTag string, workflow *WorkflowConfig) (remove, add []string) {
	remove = []string{manualTag, autoTag}
	if !isAutoProcessing {
		return remove, nil
	}
	if triggerTag != "" && !slices.ContainsFunc(remove, func(t string) bool { return strings.EqualFold(t, triggerTag) }) {
		remove = append(remove, triggerTag)
	}
	completionTag := autoTagComplete
	if workflow != nil && workflow.CompletionTag != "" {
		completionTag = workflow.CompletionTag
	}
	if completionTag != "" {
		add = []string{completionTag}
	}
	return remove, add
}
