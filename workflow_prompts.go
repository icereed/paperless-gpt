package main

import (
	"bytes"
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
