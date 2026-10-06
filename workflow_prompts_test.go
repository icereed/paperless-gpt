package main

import (
	"context"
	"errors"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowWantsOCR(t *testing.T) {
	t.Parallel()
	if workflowWantsOCR(WorkflowConfig{}) {
		t.Fatal("nil EnableOCR should be false")
	}
	off := false
	if workflowWantsOCR(WorkflowConfig{EnableOCR: &off}) {
		t.Fatal("false EnableOCR should be false")
	}
	on := true
	if !workflowWantsOCR(WorkflowConfig{EnableOCR: &on}) {
		t.Fatal("true EnableOCR should be true")
	}
}

func TestValidateWorkflowOCRConfig(t *testing.T) {
	t.Parallel()
	neg := -1
	if err := validateWorkflowOCRConfig(nil, WorkflowConfig{OCRLimitPages: &neg}); err == nil {
		t.Fatal("expected error for negative ocr_limit_pages")
	}
	zero := 0
	if err := validateWorkflowOCRConfig(nil, WorkflowConfig{OCRLimitPages: &zero}); err != nil {
		t.Fatalf("zero limit should be allowed: %v", err)
	}
	if err := validateWorkflowOCRConfig(nil, WorkflowConfig{
		Prompts: map[string]string{"ocr_prompt": "{{.Language}} {{.Content}}"},
	}); err != nil {
		t.Fatalf("valid ocr_prompt should pass without app: %v", err)
	}
	if err := validateWorkflowOCRConfig(nil, WorkflowConfig{
		Prompts: map[string]string{"ocr_prompt": "{{.Missing"},
	}); err == nil {
		t.Fatal("expected error for broken ocr_prompt template")
	}
}

func TestEffectiveOCROptionsForWorkflow(t *testing.T) {
	limitOcrPages = 5
	app := &App{
		pdfUpload:       true,
		pdfReplace:      true,
		pdfCopyMetadata: true,
		ocrProcessMode:  "image",
	}
	settingsMutex.Lock()
	prev := settings
	settings = Settings{}
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = prev
		settingsMutex.Unlock()
	})

	on := true
	pages := 1
	wf := WorkflowConfig{
		EnableOCR:     &on,
		OCRLimitPages: &pages,
		Prompts:       map[string]string{"ocr_prompt": "custom ocr {{.Content}}"},
	}
	opts := app.effectiveOCROptionsForWorkflow(wf)
	if opts.LimitPages != 1 {
		t.Fatalf("LimitPages = %d, want 1", opts.LimitPages)
	}
	if opts.UploadPDF || opts.ReplaceOriginal {
		t.Fatalf("workflow OCR must disable upload/replace, got upload=%v replace=%v", opts.UploadPDF, opts.ReplaceOriginal)
	}
	if opts.PromptOverride == "" {
		t.Fatal("expected PromptOverride from ocr_prompt")
	}
	if opts.CopyMetadata != true {
		t.Fatalf("CopyMetadata should still come from defaults, got %v", opts.CopyMetadata)
	}

	optsDefault := app.effectiveOCROptionsForWorkflow(WorkflowConfig{EnableOCR: &on})
	if optsDefault.LimitPages != 5 {
		t.Fatalf("without override LimitPages = %d, want env default 5", optsDefault.LimitPages)
	}
}

func isolateWorkflowSettings(t *testing.T) {
	t.Helper()
	settingsMutex.Lock()
	prev := settings
	settings.Workflows = nil
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = prev
		settingsMutex.Unlock()
		invalidateWorkflowTemplateCache("")
	})
}

func TestGetWorkflowTemplate(t *testing.T) {
	invalidateWorkflowTemplateCache("")
	t.Cleanup(func() { invalidateWorkflowTemplateCache("") })

	global := template.Must(template.New("title_prompt").Parse("GLOBAL {{.Content}}"))

	t.Run("override returns a distinct parsed template", func(t *testing.T) {
		wf := WorkflowConfig{ID: "inv", Prompts: map[string]string{"title_prompt": "OVERRIDE {{.Content}}"}}
		tmpl, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.NoError(t, err)
		require.NotNil(t, tmpl)
		assert.NotSame(t, global, tmpl)

		out, err := executeWorkflowTemplate(tmpl, map[string]interface{}{"Content": "doc"})
		require.NoError(t, err)
		assert.Equal(t, "OVERRIDE doc", out)
	})

	t.Run("missing or blank override falls back to global", func(t *testing.T) {
		wf := WorkflowConfig{ID: "plain", Prompts: map[string]string{"title_prompt": "  "}}
		tmpl, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.NoError(t, err)
		assert.Same(t, global, tmpl)

		wf2 := WorkflowConfig{ID: "none"}
		tmpl2, err := getWorkflowTemplate(wf2, "title_prompt", global)
		require.NoError(t, err)
		assert.Same(t, global, tmpl2)
	})

	t.Run("invalid template returns an error", func(t *testing.T) {
		wf := WorkflowConfig{ID: "bad", Prompts: map[string]string{"title_prompt": "{{.Unclosed"}}
		_, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.Error(t, err)
	})

	t.Run("cache returns the same pointer until invalidated", func(t *testing.T) {
		wf := WorkflowConfig{ID: "cached", Prompts: map[string]string{"title_prompt": "CACHED {{.Content}}"}}
		first, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.NoError(t, err)
		second, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.NoError(t, err)
		assert.Same(t, first, second)

		invalidateWorkflowTemplateCache("cached")
		third, err := getWorkflowTemplate(wf, "title_prompt", global)
		require.NoError(t, err)
		assert.NotSame(t, first, third)
	})
}

func TestResolveGenerationFlags(t *testing.T) {
	base := GenerateSuggestionsRequest{
		GenerateTitles:         true,
		GenerateTags:           true,
		GenerateCorrespondents: false,
		GenerateDocumentTypes:  true,
		GenerateCreatedDate:    false,
		GenerateCustomFields:   true,
	}
	inherit := resolveGenerationFlags(base, WorkflowConfig{})
	assert.Equal(t, base.GenerateTitles, inherit.GenerateTitles)
	assert.Equal(t, base.GenerateTags, inherit.GenerateTags)
	assert.Equal(t, base.GenerateCorrespondents, inherit.GenerateCorrespondents)
	assert.Equal(t, base.GenerateCustomFields, inherit.GenerateCustomFields)

	off := false
	on := true
	over := resolveGenerationFlags(base, WorkflowConfig{
		GenerateTitles: &off,
		GenerateTags:   &on,
	})
	assert.False(t, over.GenerateTitles)
	assert.True(t, over.GenerateTags)
	assert.Equal(t, base.GenerateCorrespondents, over.GenerateCorrespondents)
}

func TestGenerateWorkflowID(t *testing.T) {
	assert.Equal(t, "paperless-gpt-invoices", generateWorkflowID("paperless-gpt-invoices", nil))
	assert.Equal(t, "invoices-type-a", generateWorkflowID("Invoices Type A", nil))
	assert.Equal(t, "workflow", generateWorkflowID("!!!", nil))
	existing := []WorkflowConfig{{ID: "invoices"}}
	assert.Equal(t, "invoices-2", generateWorkflowID("invoices", existing))
}

func TestSystemTagsIncludesWorkflowTags(t *testing.T) {
	isolateWorkflowSettings(t)
	prev := struct{ manual, auto, ocrAuto, fail, complete, ocrComplete string }{
		manualTag, autoTag, autoOcrTag, failTag, autoTagComplete, pdfOCRCompleteTag,
	}
	manualTag, autoTag, autoOcrTag = "paperless-gpt", "paperless-gpt-auto", "paperless-gpt-ocr-auto"
	failTag, autoTagComplete, pdfOCRCompleteTag = "paperless-gpt-failed", "paperless-gpt-auto-complete", "paperless-gpt-ocr-complete"
	t.Cleanup(func() {
		manualTag, autoTag, autoOcrTag = prev.manual, prev.auto, prev.ocrAuto
		failTag, autoTagComplete, pdfOCRCompleteTag = prev.fail, prev.complete, prev.ocrComplete
	})

	settingsMutex.Lock()
	settings.Workflows = []WorkflowConfig{{
		ID:            "inv",
		TriggerTag:    "paperless-gpt-invoices",
		CompletionTag: "paperless-gpt-invoices-done",
	}, {
		ID:         "dup",
		TriggerTag: "paperless-gpt-auto", // already a global system tag
	}, {
		ID:         "empty",
		TriggerTag: "",
	}}
	settingsMutex.Unlock()

	got := systemTags()
	assert.Contains(t, got, "paperless-gpt-invoices")
	assert.Contains(t, got, "paperless-gpt-invoices-done")
	assert.Contains(t, got, "paperless-gpt-auto")
	autoCount := 0
	for _, tag := range got {
		if tag == "paperless-gpt-auto" {
			autoCount++
		}
	}
	assert.Equal(t, 1, autoCount, "duplicate AUTO_TAG from a workflow must be collapsed")
	assert.NotContains(t, got, "")
}

func TestEnsureWorkflowTagsExist(t *testing.T) {
	isolateWorkflowSettings(t)
	settingsMutex.Lock()
	settings.Workflows = []WorkflowConfig{{
		ID:            "inv",
		TriggerTag:    "paperless-gpt-invoices",
		CompletionTag: "paperless-gpt-invoices-done",
	}}
	settingsMutex.Unlock()

	var ensured []string
	ensureWorkflowTagsExist(context.Background(), func(_ context.Context, tag string) error {
		ensured = append(ensured, tag)
		return nil
	})
	assert.Equal(t, []string{"paperless-gpt-invoices", "paperless-gpt-invoices-done"}, ensured)

	err := errors.New("paperless unreachable")
	ensureWorkflowTagsExist(context.Background(), func(_ context.Context, tag string) error {
		return err
	})
}
