package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// workflowTemplateCache caches compiled per-workflow templates so they are not
// re-parsed on every document. The cache is invalidated whenever a workflow is
// saved via the API.
var (
	workflowTemplateCacheMu sync.RWMutex
	workflowTemplateCache   = map[string]map[string]*template.Template{} // workflowID → promptName → template
)

// invalidateWorkflowTemplateCache drops all cached templates for a specific
// workflow (or all workflows when id is "").
func invalidateWorkflowTemplateCache(id string) {
	workflowTemplateCacheMu.Lock()
	defer workflowTemplateCacheMu.Unlock()
	if id == "" {
		workflowTemplateCache = map[string]map[string]*template.Template{}
	} else {
		delete(workflowTemplateCache, id)
	}
}

// getWorkflowTemplate returns the compiled template for a workflow + prompt
// name. Falls back to the global template when the workflow has no override.
// globalTmpl must be non-nil.
func getWorkflowTemplate(wf WorkflowConfig, promptName string, globalTmpl *template.Template) (*template.Template, error) {
	raw, hasOverride := wf.Prompts[promptName]
	if !hasOverride || strings.TrimSpace(raw) == "" {
		return globalTmpl, nil
	}

	// Try cache first.
	workflowTemplateCacheMu.RLock()
	if byName, ok := workflowTemplateCache[wf.ID]; ok {
		if tmpl, ok := byName[promptName]; ok {
			workflowTemplateCacheMu.RUnlock()
			return tmpl, nil
		}
	}
	workflowTemplateCacheMu.RUnlock()

	// Parse and cache.
	tmpl, err := template.New(promptName).Funcs(sprig.FuncMap()).Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("workflow %q: invalid template for %q: %w", wf.ID, promptName, err)
	}

	workflowTemplateCacheMu.Lock()
	if workflowTemplateCache[wf.ID] == nil {
		workflowTemplateCache[wf.ID] = map[string]*template.Template{}
	}
	workflowTemplateCache[wf.ID][promptName] = tmpl
	workflowTemplateCacheMu.Unlock()

	return tmpl, nil
}

// executeWorkflowTemplate is a helper that executes a (possibly workflow-
// overridden) template against templateData and returns the rendered prompt.
func executeWorkflowTemplate(tmpl *template.Template, data map[string]interface{}) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// getWorkflowByID looks up a workflow by its ID in the current settings. It
// returns a zero-value WorkflowConfig and false when not found.
func getWorkflowByID(id string) (WorkflowConfig, bool) {
	if id == "" {
		return WorkflowConfig{}, false
	}
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()
	for _, wf := range settings.Workflows {
		if wf.ID == id {
			return wf, true
		}
	}
	return WorkflowConfig{}, false
}

// workflowManagedTags returns trigger and completion tags from configured
// workflows. Empty names are omitted. The caller must not hold settingsMutex
// exclusively; this takes a read lock.
func workflowManagedTags() []string {
	settingsMutex.RLock()
	defer settingsMutex.RUnlock()
	return workflowManagedTagsLocked()
}

func workflowManagedTagsLocked() []string {
	var tags []string
	for _, wf := range settings.Workflows {
		if wf.TriggerTag != "" {
			tags = append(tags, wf.TriggerTag)
		}
		if wf.CompletionTag != "" {
			tags = append(tags, wf.CompletionTag)
		}
	}
	return tags
}

// ensureWorkflowTagsExist creates each workflow trigger/completion tag in
// paperless-ngx so name→ID resolution does not silently drop them.
func ensureWorkflowTagsExist(ctx context.Context, ensure func(context.Context, string) error) {
	if ensure == nil {
		return
	}
	for _, tag := range workflowManagedTags() {
		if err := ensure(ctx, tag); err != nil {
			log.Warnf("Failed to ensure workflow tag %q exists: %v. Processing will still run, but applying this tag may fail until it exists in paperless-ngx.", tag, err)
		}
	}
}

// workflowWantsOCR reports whether a workflow should OCR before metadata.
func workflowWantsOCR(wf WorkflowConfig) bool {
	return wf.EnableOCR != nil && *wf.EnableOCR
}

// validateWorkflowOCRConfig checks OCR-related workflow fields before save.
// app may be nil when only structural checks are needed (e.g. unit tests).
func validateWorkflowOCRConfig(app *App, wf WorkflowConfig) error {
	if wf.OCRLimitPages != nil && *wf.OCRLimitPages < 0 {
		return fmt.Errorf("ocr_limit_pages must be 0 (no limit) or positive")
	}
	prompt := ""
	if wf.Prompts != nil {
		prompt = strings.TrimSpace(wf.Prompts["ocr_prompt"])
	}
	if prompt == "" {
		return nil
	}
	if app != nil && !app.ocrSupportsPromptOverride() {
		return fmt.Errorf("ocr_prompt overrides are only supported by the LLM OCR provider")
	}
	if _, err := renderOCRPromptOverride(prompt, ""); err != nil {
		return fmt.Errorf("ocr_prompt does not render: %w", err)
	}
	return nil
}

// resolveGenerationFlags returns a GenerateSuggestionsRequest whose bool flags
// are resolved from the workflow overrides (if any) on top of the global
// defaults encoded in the base request.
func resolveGenerationFlags(base GenerateSuggestionsRequest, wf WorkflowConfig) GenerateSuggestionsRequest {
	r := base
	if wf.GenerateTitles != nil {
		r.GenerateTitles = *wf.GenerateTitles
	}
	if wf.GenerateTags != nil {
		r.GenerateTags = *wf.GenerateTags
	}
	if wf.GenerateCorrespondents != nil {
		r.GenerateCorrespondents = *wf.GenerateCorrespondents
	}
	if wf.GenerateCreatedDate != nil {
		r.GenerateCreatedDate = *wf.GenerateCreatedDate
	}
	if wf.GenerateDocumentTypes != nil {
		r.GenerateDocumentTypes = *wf.GenerateDocumentTypes
	}
	if wf.GenerateCustomFields != nil {
		r.GenerateCustomFields = *wf.GenerateCustomFields
	}
	return r
}
