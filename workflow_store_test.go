package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestWorkflowStoreRoundTrip(t *testing.T) {
	store := newWorkflowStore(t.TempDir())
	pages := 3
	wf := WorkflowConfig{
		ID:             "invoices",
		Name:           "Invoices",
		TriggerTag:     "invoices",
		CompletionTag:  "invoices-done",
		GenerateTitles: boolPtr(true),
		GenerateTags:   boolPtr(false),
		EnableOCR:      boolPtr(true),
		OCRLimitPages:  &pages,
		Prompts: map[string]string{
			"title_prompt": "Title for {{.Content}}",
			"date_prompt":  "Date of {{.Content}}",
		},
	}
	_, err := store.Save(wf, true, nil)
	require.NoError(t, err)

	got, ok := store.Get("invoices")
	require.True(t, ok)
	assert.Equal(t, wf, got)

	// Prompts are plain template files named like the global ones, so a
	// global prompt can be copied in as is.
	content, err := os.ReadFile(filepath.Join(store.dir, "invoices", "created_date_prompt.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, "Date of {{.Content}}", string(content))
	assert.NoFileExists(t, filepath.Join(store.dir, "invoices", "tag_prompt.tmpl"))

	var onDisk map[string]any
	data, err := os.ReadFile(filepath.Join(store.dir, "invoices", workflowFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &onDisk))
	assert.EqualValues(t, 1, onDisk["version"])
	assert.NotContains(t, onDisk, "id", "the directory name is the ID")
	assert.NotContains(t, onDisk, "prompts", "prompts live in their own files")

	// Removing a prompt override removes its file.
	wf.Prompts = map[string]string{"title_prompt": "Title for {{.Content}}"}
	_, err = store.Save(wf, false, nil)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(store.dir, "invoices", "created_date_prompt.tmpl"))

	_, err = store.Delete("invoices")
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(store.dir, "invoices"))
	assert.Empty(t, store.List())
}

func TestWorkflowStorePicksUpHandEditedFiles(t *testing.T) {
	store := newWorkflowStore(t.TempDir())
	assert.Empty(t, store.List())

	dir := filepath.Join(store.dir, "contracts")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, workflowFileName),
		[]byte(`{"version": 1, "name": "Contracts", "trigger_tag": "contracts"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "title_prompt.tmpl"), []byte("v1 {{.Content}}"), 0o644))

	got, ok := store.Get("contracts")
	require.True(t, ok, "a workflow created by hand is found without a restart")
	assert.Equal(t, "v1 {{.Content}}", got.Prompts["title_prompt"])

	// Some file systems only keep whole seconds; make sure the change is seen.
	later := time.Now().Add(2 * time.Second)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "title_prompt.tmpl"), []byte("v2 {{.Content}}"), 0o644))
	require.NoError(t, os.Chtimes(filepath.Join(dir, "title_prompt.tmpl"), later, later))
	got, _ = store.Get("contracts")
	assert.Equal(t, "v2 {{.Content}}", got.Prompts["title_prompt"])
}

func TestWorkflowStoreSkipsBrokenWorkflows(t *testing.T) {
	store := newWorkflowStore(t.TempDir())
	write := func(dir, content string) {
		require.NoError(t, os.MkdirAll(filepath.Join(store.dir, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(store.dir, dir, workflowFileName), []byte(content), 0o644))
	}
	write("good", `{"version": 1, "trigger_tag": "good"}`)
	write("broken-json", `{"trigger_tag": `)
	write("no-trigger", `{"version": 1, "name": "x"}`)
	write("newer", `{"version": 2, "trigger_tag": "newer"}`)
	write("Bad Name", `{"version": 1, "trigger_tag": "bad"}`)
	require.NoError(t, os.MkdirAll(filepath.Join(store.dir, "no-file"), 0o755))

	list := store.List()
	require.Len(t, list, 1, "one broken workflow must not take the others down")
	assert.Equal(t, "good", list[0].ID)
}

func TestWorkflowStoreRejectsUnsafeIDs(t *testing.T) {
	store := newWorkflowStore(t.TempDir())
	for _, id := range []string{"../escape", "a/b", "", "UPPER", ".hidden"} {
		status, err := store.Save(WorkflowConfig{ID: id, TriggerTag: "x"}, true, nil)
		assert.Error(t, err, id)
		assert.Equal(t, http.StatusBadRequest, status, id)
	}
	entries, _ := os.ReadDir(filepath.Dir(store.dir))
	assert.Len(t, entries, 1, "nothing may be written outside the store")
}

func TestMigrateWorkflowsFromSettings(t *testing.T) {
	isolateWorkflowSettings(t)
	t.Chdir(t.TempDir()) // settings.json is written relative to the working directory
	settingsMutex.Lock()
	previous := settings
	settings.Workflows = []WorkflowConfig{
		{ID: "invoices", Name: "Invoices", TriggerTag: "invoices", Prompts: map[string]string{"title_prompt": "T {{.Content}}"}},
		{ID: "", TriggerTag: "Private Mail"},
	}
	settingsMutex.Unlock()
	t.Cleanup(func() {
		settingsMutex.Lock()
		settings = previous
		settingsMutex.Unlock()
	})

	migrateWorkflowsFromSettings(workflows)

	got := workflows.List()
	require.Len(t, got, 2)
	assert.Equal(t, "invoices", got[0].ID)
	assert.Equal(t, "T {{.Content}}", got[0].Prompts["title_prompt"])
	assert.Equal(t, "private-mail", got[1].ID, "a workflow without ID gets one")
	settingsMutex.RLock()
	assert.Empty(t, settings.Workflows, "migrated workflows leave settings.json")
	settingsMutex.RUnlock()

	// Running it again (an earlier start wrote the files but could not update
	// settings.json) must not duplicate anything.
	settingsMutex.Lock()
	settings.Workflows = []WorkflowConfig{{ID: "invoices", TriggerTag: "invoices"}}
	settingsMutex.Unlock()
	migrateWorkflowsFromSettings(workflows)
	assert.Len(t, workflows.List(), 2)
	settingsMutex.RLock()
	assert.Empty(t, settings.Workflows)
	settingsMutex.RUnlock()
}

func TestTriggerTagRouter(t *testing.T) {
	prevAutoTag := autoTag
	autoTag = "paperless-gpt-auto"
	t.Cleanup(func() { autoTag = prevAutoTag })
	isolateWorkflowSettings(t)
	useWorkflows(t,
		WorkflowConfig{ID: "invoices", TriggerTag: "Invoices"},
		WorkflowConfig{ID: "contracts", TriggerTag: "contracts"},
	)
	router := triggerTagRouter{store: workflows}

	assert.Equal(t, []string{"contracts", "Invoices", "paperless-gpt-auto"}, router.PollTags(),
		"workflow triggers come before AUTO_TAG, so a document carrying both is processed by its workflow")

	wf := router.WorkflowFor(Document{}, "invoices")
	require.NotNil(t, wf, "trigger tags match case-insensitively")
	assert.Equal(t, "invoices", wf.ID)
	assert.Nil(t, router.WorkflowFor(Document{}, "paperless-gpt-auto"))
	assert.Nil(t, router.WorkflowFor(Document{}, "unknown"))
}

// fixedRouter routes every document to one workflow, the way a custom
// router plugged into App.WorkflowRouter could.
type fixedRouter struct {
	tag string
	wf  WorkflowConfig
}

func (r fixedRouter) PollTags() []string { return []string{r.tag} }
func (r fixedRouter) WorkflowFor(Document, string) *WorkflowConfig {
	wf := r.wf
	return &wf
}

func TestProcessAutoTagDocumentsUsesTheRouter(t *testing.T) {
	prevAutoTag, prevFailTag, prevRetries := autoTag, failTag, autoTagMaxRetries
	t.Cleanup(func() { autoTag, failTag, autoTagMaxRetries = prevAutoTag, prevFailTag, prevRetries })
	autoTag, failTag, autoTagMaxRetries = "paperless-gpt-auto", "paperless-gpt-failed", 1

	client := &recordingClient{
		taggedDocuments: map[string][]Document{
			"routed": {{ID: 7, Title: "Doc", Tags: []string{"routed"}}},
		},
	}
	app := &App{Client: client, WorkflowRouter: fixedRouter{tag: "routed", wf: WorkflowConfig{ID: "custom", TriggerTag: "other", EnableOCR: boolPtr(true)}}}

	_, _ = app.processAutoTagDocuments(context.Background())
	assert.Equal(t, []string{"routed"}, client.tagFetches, "only the router's tags are polled")
	require.Len(t, client.calls, 1)
	assert.Equal(t, []string{"routed"}, client.calls[0].RemoveTags, "the polled tag comes off, not the workflow's own trigger")
}

func TestWorkflowPreviewHandler(t *testing.T) {
	router := setupTestRouter(t)
	isolateWorkflowSettings(t)
	app := &App{}
	router.POST("/api/workflows/preview", app.workflowPreviewHandler)

	post := func(body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
		req, err := http.NewRequest(http.MethodPost, "/api/workflows/preview", &buf)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	w := post(map[string]any{"workflow": WorkflowConfig{TriggerTag: "x"}})
	assert.Equal(t, http.StatusBadRequest, w.Code, "a document is required")

	w = post(map[string]any{
		"workflow":    WorkflowConfig{TriggerTag: "x", Prompts: map[string]string{"title_prompt": "{{.Unclosed"}},
		"document_id": 1,
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, "a broken prompt is reported before any LLM call")
	assert.Contains(t, w.Body.String(), "title_prompt")
}

func TestValidatePromptTemplates(t *testing.T) {
	assert.NoError(t, validatePromptTemplates(WorkflowConfig{Prompts: map[string]string{"title_prompt": "{{.Content | trunc 10}}"}}),
		"sprig functions are available, as in the global prompts")
	assert.ErrorContains(t, validatePromptTemplates(WorkflowConfig{Prompts: map[string]string{"title_promt": "x"}}), "unknown prompt")
	assert.ErrorContains(t, validatePromptTemplates(WorkflowConfig{Prompts: map[string]string{"tag_prompt": "{{if}}"}}), "tag_prompt")
}

func TestWorkflowStoreOnChangeSeesHandEditedWorkflows(t *testing.T) {
	store := newWorkflowStore(t.TempDir())
	changed := make(chan []WorkflowConfig, 4)
	store.OnChange(func(wfs []WorkflowConfig) { changed <- wfs })

	dir := filepath.Join(store.dir, "hand")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, workflowFileName), []byte(`{"trigger_tag": "hand"}`), 0o644))
	store.List()

	select {
	case wfs := <-changed:
		require.Len(t, wfs, 1)
		assert.Equal(t, "hand", wfs[0].TriggerTag)
	case <-time.After(2 * time.Second):
		t.Fatal("OnChange was not called for a workflow added by hand")
	}
}
