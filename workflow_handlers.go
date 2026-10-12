package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/gin-gonic/gin"
)

// listWorkflowsHandler handles GET /api/workflows.
func (app *App) listWorkflowsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, workflows.List())
}

// createWorkflowHandler handles POST /api/workflows.
func (app *App) createWorkflowHandler(c *gin.Context) {
	var wf WorkflowConfig
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	wf = normalizeWorkflow(wf)
	if err := app.validateWorkflowConfig(wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if wf.ID == "" {
		wf.ID = generateWorkflowID(wf.TriggerTag, workflows.List())
	}
	if status, err := workflows.Save(wf, true, validateWorkflowTags); err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	invalidateWorkflowTemplateCache(wf.ID)
	app.ensureWorkflowTags(c.Request.Context(), wf)
	saved, _ := workflows.Get(wf.ID)
	c.JSON(http.StatusCreated, saved)
}

// updateWorkflowHandler handles PUT /api/workflows/:id.
func (app *App) updateWorkflowHandler(c *gin.Context) {
	var wf WorkflowConfig
	if err := c.ShouldBindJSON(&wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	wf.ID = c.Param("id") // the path decides which workflow is replaced
	wf = normalizeWorkflow(wf)
	if err := app.validateWorkflowConfig(wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if status, err := workflows.Save(wf, false, validateWorkflowTags); err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	invalidateWorkflowTemplateCache(wf.ID)
	app.ensureWorkflowTags(c.Request.Context(), wf)
	saved, _ := workflows.Get(wf.ID)
	c.JSON(http.StatusOK, saved)
}

// deleteWorkflowHandler handles DELETE /api/workflows/:id.
func (app *App) deleteWorkflowHandler(c *gin.Context) {
	id := c.Param("id")
	if status, err := workflows.Delete(id); err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	invalidateWorkflowTemplateCache(id)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("workflow %q deleted", id)})
}

// workflowDocumentsHandler handles GET /api/workflows/:id/documents: the
// documents currently waiting for the workflow, at most one page.
func (app *App) workflowDocumentsHandler(c *gin.Context) {
	wf, ok := workflows.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("workflow %q not found", c.Param("id"))})
		return
	}
	docs, err := app.Client.GetDocumentsByTag(c.Request.Context(), wf.TriggerTag, workflowQueuePageSize)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Could not ask paperless-ngx for documents tagged %q: %v", wf.TriggerTag, err)})
		return
	}
	type waitingDocument struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}
	out := make([]waitingDocument, 0, len(docs))
	for _, d := range docs {
		out = append(out, waitingDocument{ID: d.ID, Title: d.Title})
	}
	c.JSON(http.StatusOK, gin.H{"documents": out, "more": len(docs) >= workflowQueuePageSize})
}

// workflowQueuePageSize matches the page the background processor fetches.
const workflowQueuePageSize = 25

// workflowPreviewHandler handles POST /api/workflows/preview. It runs a
// workflow, saved or not, on one document and returns what it would set,
// without changing anything in paperless-ngx.
func (app *App) workflowPreviewHandler(c *gin.Context) {
	var req struct {
		Workflow   WorkflowConfig `json:"workflow"`
		DocumentID int            `json:"document_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DocumentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Send a workflow and a document_id"})
		return
	}
	wf := normalizeWorkflow(req.Workflow)
	if err := validatePromptTemplates(wf); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	doc, err := app.Client.GetDocument(ctx, req.DocumentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Document %d could not be loaded from paperless-ngx: %v", req.DocumentID, err)})
		return
	}
	request := resolveGenerationFlags(globalAutoProcessingRequest(doc), wf)
	request.Workflow = &wf
	request.IsAutoProcessing = false // a preview hands over no tags
	suggestions, err := app.generateDocumentSuggestions(ctx, request, documentLogger(doc.ID))
	if err != nil || len(suggestions) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("The LLM call failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"suggestion": suggestions[0],
		"steps": gin.H{
			"generate_titles":         request.GenerateTitles,
			"generate_tags":           request.GenerateTags,
			"generate_correspondents": request.GenerateCorrespondents,
			"generate_document_types": request.GenerateDocumentTypes,
			"generate_created_date":   request.GenerateCreatedDate,
			"generate_custom_fields":  request.GenerateCustomFields,
		},
		"ocr_skipped": workflowWantsOCR(wf),
	})
}

// workflowDefaultsHandler handles GET /api/workflows/defaults: what a
// workflow inherits when it does not override a setting, so the editor can
// show it.
func (app *App) workflowDefaultsHandler(c *gin.Context) {
	base := globalAutoProcessingRequest(Document{})
	prompts := map[string]string{}
	for key, file := range workflowPromptFiles {
		if content, err := os.ReadFile(filepath.Join("prompts", file)); err == nil {
			prompts[key] = string(content)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"auto_tag":                      autoTag,
		"auto_tag_complete":             app.autoTagComplete,
		"storage_dir":                   workflows.dir,
		"ocr_enabled":                   app.isOcrEnabled(),
		"ocr_prompt_override_supported": app.ocrProvider != nil && app.ocrSupportsPromptOverride(),
		"generate": gin.H{
			"generate_titles":         base.GenerateTitles,
			"generate_tags":           base.GenerateTags,
			"generate_correspondents": base.GenerateCorrespondents,
			"generate_document_types": base.GenerateDocumentTypes,
			"generate_created_date":   base.GenerateCreatedDate,
			"generate_custom_fields":  base.GenerateCustomFields,
		},
		"prompts": prompts,
	})
}

// globalAutoProcessingRequest is the request the default AUTO_TAG path uses
// for a document: the AUTO_GENERATE_* settings and the custom field setting.
func globalAutoProcessingRequest(doc Document) GenerateSuggestionsRequest {
	settingsMutex.RLock()
	generateCustomFields := settings.CustomFieldsEnable
	settingsMutex.RUnlock()
	return GenerateSuggestionsRequest{
		Documents:              []Document{doc},
		GenerateTitles:         strings.ToLower(autoGenerateTitle) != "false",
		GenerateTags:           strings.ToLower(autoGenerateTags) != "false",
		GenerateCorrespondents: strings.ToLower(autoGenerateCorrespondents) != "false",
		GenerateDocumentTypes:  strings.ToLower(autoGenerateDocumentType) != "false",
		GenerateCreatedDate:    strings.ToLower(autoGenerateCreatedDate) != "false",
		GenerateCustomFields:   generateCustomFields,
	}
}

// normalizeWorkflow trims the tag names so " invoices" and "invoices" are
// the same tag, as they are in paperless-ngx, and drops empty prompts.
func normalizeWorkflow(wf WorkflowConfig) WorkflowConfig {
	wf.ID = strings.TrimSpace(wf.ID)
	wf.Name = strings.TrimSpace(wf.Name)
	wf.TriggerTag = strings.TrimSpace(wf.TriggerTag)
	wf.CompletionTag = strings.TrimSpace(wf.CompletionTag)
	for key, prompt := range wf.Prompts {
		if strings.TrimSpace(prompt) == "" {
			delete(wf.Prompts, key)
		}
	}
	return wf
}

// validateWorkflowConfig checks what can be checked without the other
// workflows: prompts and OCR settings.
func (app *App) validateWorkflowConfig(wf WorkflowConfig) error {
	if err := validatePromptTemplates(wf); err != nil {
		return err
	}
	return validateWorkflowOCRConfig(app, wf)
}

// validatePromptTemplates rejects prompt keys that do not exist and
// templates that do not parse. Without this, a typo in a prompt only shows
// up as a warning in the log while documents silently use the global prompt.
func validatePromptTemplates(wf WorkflowConfig) error {
	for key, prompt := range wf.Prompts {
		if _, known := workflowPromptFiles[key]; !known {
			return fmt.Errorf("unknown prompt %q", key)
		}
		if key == "ocr_prompt" {
			continue // checked by validateWorkflowOCRConfig, which renders it
		}
		if _, err := template.New(key).Funcs(sprig.FuncMap()).Parse(prompt); err != nil {
			return fmt.Errorf("%s does not parse: %w", key, err)
		}
	}
	return nil
}

// validateWorkflowTags checks a workflow's tags against the global tags and
// the other workflows. Tag names compare case-insensitively, like
// paperless-ngx does. Every rejected case would otherwise make documents
// loop: a completion tag that is also a trigger tag sends the document
// straight back into processing, and a trigger tag that paperless-gpt adds
// or removes itself gets fought over by two processing paths.
func validateWorkflowTags(wf WorkflowConfig, others []WorkflowConfig) (int, error) {
	if wf.TriggerTag == "" {
		return http.StatusBadRequest, fmt.Errorf("trigger_tag is required")
	}
	reserved := []struct{ env, tag string }{
		{"AUTO_TAG", autoTag},
		{"MANUAL_TAG", manualTag},
		{"AUTO_OCR_TAG", autoOcrTag},
		{"FAIL_TAG", failTag},
		{"AUTO_TAG_COMPLETE", autoTagComplete},
		{"PDF_OCR_COMPLETE_TAG", pdfOCRCompleteTag},
	}
	for _, r := range reserved {
		if r.tag == "" {
			continue
		}
		if strings.EqualFold(wf.TriggerTag, r.tag) {
			return http.StatusBadRequest, fmt.Errorf("trigger_tag %q is already used by paperless-gpt as %s", wf.TriggerTag, r.env)
		}
		// A completion tag may share AUTO_TAG_COMPLETE or the OCR complete
		// tag, but never a tag that starts processing.
		if r.env != "AUTO_TAG_COMPLETE" && r.env != "PDF_OCR_COMPLETE_TAG" && strings.EqualFold(wf.CompletionTag, r.tag) {
			return http.StatusBadRequest, fmt.Errorf("completion_tag %q is already used by paperless-gpt as %s", wf.CompletionTag, r.env)
		}
	}
	if strings.EqualFold(wf.CompletionTag, wf.TriggerTag) {
		return http.StatusBadRequest, fmt.Errorf("completion_tag must differ from trigger_tag, otherwise the document is processed again and again")
	}
	for _, other := range others {
		if strings.EqualFold(other.TriggerTag, wf.TriggerTag) {
			return http.StatusConflict, fmt.Errorf("trigger_tag %q is already used by workflow %q", wf.TriggerTag, other.ID)
		}
		if wf.CompletionTag != "" && strings.EqualFold(other.TriggerTag, wf.CompletionTag) {
			return http.StatusConflict, fmt.Errorf("completion_tag %q is the trigger_tag of workflow %q; chaining workflows is not supported", wf.CompletionTag, other.ID)
		}
		if other.CompletionTag != "" && strings.EqualFold(other.CompletionTag, wf.TriggerTag) {
			return http.StatusConflict, fmt.Errorf("trigger_tag %q is the completion_tag of workflow %q; chaining workflows is not supported", wf.TriggerTag, other.ID)
		}
	}
	return http.StatusOK, nil
}

// ensureWorkflowTags creates a saved workflow's tags in paperless-ngx, as
// startup does for the workflows already configured. Without it a new
// completion tag would fail to apply until the next restart.
func (app *App) ensureWorkflowTags(ctx context.Context, wf WorkflowConfig) {
	ensurer, ok := app.Client.(interface {
		EnsureTagExists(context.Context, string) error
	})
	if !ok {
		return
	}
	for _, tag := range []string{wf.TriggerTag, wf.CompletionTag} {
		if tag == "" {
			continue
		}
		if err := ensurer.EnsureTagExists(ctx, tag); err != nil {
			log.Warnf("Failed to ensure workflow tag %q exists: %v", tag, err)
		}
	}
}

// generateWorkflowID derives a URL- and file-system-safe workflow ID from the
// trigger tag, appending a number when it is taken.
func generateWorkflowID(triggerTag string, existing []WorkflowConfig) string {
	base := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32 // to lower
		}
		return '-'
	}, triggerTag)
	base = strings.Trim(base, "-")
	if len(base) > 56 {
		base = strings.Trim(base[:56], "-")
	}
	if base == "" {
		base = "workflow"
	}

	used := map[string]bool{}
	for _, wf := range existing {
		used[wf.ID] = true
	}
	id := base
	for i := 2; used[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	return id
}
