package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// fakePaperless is a stateful stand-in for paperless-ngx holding one
// document. Unlike the canned responses elsewhere in the tests it applies
// writes to its state, so a test can change the document "server-side" while
// paperless-gpt works on it and check what survives.
type fakePaperless struct {
	mu      sync.Mutex
	tags    map[string]int // tag catalog: name -> ID
	docID   int
	docTags []int
	content string
	patches []map[string]any // PATCH bodies sent to the document
	bulk    []map[string]any // bulk_edit bodies
	ops     []string         // "PATCH" / "bulk_edit", in order

	patchStatus int // when non-zero, PATCH answers with this status
	bulkStatus  int // when non-zero, bulk_edit answers with this status

	parent map[int]int // tag hierarchy: child ID -> parent ID
}

func (f *fakePaperless) tagName(id int) string {
	for name, tagID := range f.tags {
		if tagID == id {
			return name
		}
	}
	return ""
}

// docTagNames returns the document's current tag names, sorted.
func (f *fakePaperless) docTagNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.docTags))
	for _, id := range f.docTags {
		names = append(names, f.tagName(id))
	}
	slices.Sort(names)
	return names
}

// addTag adds a tag to the document the way a user, a paperless-ngx workflow
// or a script would: directly in paperless-ngx, not through paperless-gpt.
func (f *fakePaperless) addTag(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docTags = append(f.docTags, f.tags[name])
}

// removeTag removes a tag from the document directly in paperless-ngx.
func (f *fakePaperless) removeTag(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docTags = slices.DeleteFunc(f.docTags, func(id int) bool { return id == f.tags[name] })
}

func (f *fakePaperless) register(env *testEnv) {
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		type tagResult struct {
			ID            int    `json:"id"`
			Name          string `json:"name"`
			DocumentCount int    `json:"document_count"`
		}
		var results []tagResult
		filter := r.URL.Query().Get("name__iexact")
		for name, id := range f.tags {
			if filter != "" && !strings.EqualFold(filter, name) {
				continue
			}
			count := 0
			if slices.Contains(f.docTags, id) {
				count = 1
			}
			results = append(results, tagResult{ID: id, Name: name, DocumentCount: count})
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "results": results})
	})

	docJSON := func() GetDocumentApiResponseResult {
		return GetDocumentApiResponseResult{
			ID:          f.docID,
			Title:       "Scan",
			Tags:        slices.Clone(f.docTags),
			Content:     f.content,
			CreatedDate: "2026-09-02",
		}
	}

	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		resp := GetDocumentsApiResponse{}
		filter := r.URL.Query().Get("tags__name__iexact")
		if id, ok := f.tags[filter]; ok && slices.Contains(f.docTags, id) {
			resp.Results = append(resp.Results, docJSON())
			resp.Count = 1
		}
		json.NewEncoder(w).Encode(resp)
	})

	env.setMockResponse("/api/documents/"+strconv.Itoa(f.docID)+"/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(docJSON())
		case http.MethodPatch:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.patches = append(f.patches, body)
			f.ops = append(f.ops, "PATCH")
			if f.patchStatus != 0 {
				http.Error(w, `{"detail":"rejected"}`, f.patchStatus)
				return
			}
			if tags, ok := body["tags"].([]any); ok {
				f.docTags = nil
				for _, t := range tags {
					f.docTags = append(f.docTags, int(t.(float64)))
				}
			}
			if content, ok := body["content"].(string); ok {
				f.content = content
			}
			json.NewEncoder(w).Encode(docJSON())
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	})

	env.setMockResponse("/api/documents/bulk_edit/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body struct {
			Documents  []int  `json:"documents"`
			Method     string `json:"method"`
			Parameters struct {
				AddTags    []int `json:"add_tags"`
				RemoveTags []int `json:"remove_tags"`
			} `json:"parameters"`
		}
		raw, _ := io.ReadAll(r.Body)
		var rawMap map[string]any
		if err := json.Unmarshal(raw, &rawMap); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.bulk = append(f.bulk, rawMap)
		f.ops = append(f.ops, "bulk_edit")
		if f.bulkStatus != 0 {
			http.Error(w, "bulk edit failed", f.bulkStatus)
			return
		}
		_ = json.Unmarshal(raw, &body)
		if r.Method != http.MethodPost || body.Method != "modify_tags" || !slices.Equal(body.Documents, []int{f.docID}) {
			http.Error(w, "unexpected bulk edit", http.StatusBadRequest)
			return
		}
		// Like paperless-ngx' serializer: both lists must be lists (not
		// null) of existing, distinct tag IDs.
		params, _ := rawMap["parameters"].(map[string]any)
		for _, key := range []string{"add_tags", "remove_tags"} {
			if _, isList := params[key].([]any); !isList {
				http.Error(w, key+" must be a list", http.StatusBadRequest)
				return
			}
		}
		for _, ids := range [][]int{body.Parameters.AddTags, body.Parameters.RemoveTags} {
			seen := map[int]bool{}
			for _, id := range ids {
				if seen[id] || f.tagName(id) == "" {
					http.Error(w, "Some tags don't exist or were specified twice.", http.StatusBadRequest)
					return
				}
				seen[id] = true
			}
		}
		// Same as paperless-ngx: remove (with descendants) first, then add
		// (with ancestors).
		isUnder := func(id, ancestor int) bool {
			for p, ok := f.parent[id]; ok; p, ok = f.parent[p] {
				if p == ancestor {
					return true
				}
			}
			return false
		}
		f.docTags = slices.DeleteFunc(f.docTags, func(id int) bool {
			for _, removed := range body.Parameters.RemoveTags {
				if id == removed || isUnder(id, removed) {
					return true
				}
			}
			return false
		})
		for _, id := range body.Parameters.AddTags {
			for ; ; id = f.parent[id] {
				if !slices.Contains(f.docTags, id) {
					f.docTags = append(f.docTags, id)
				}
				if _, ok := f.parent[id]; !ok {
					break
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]string{"result": "OK"})
	})
}

// hookDocumentProcessor runs during() while "OCR" is in progress, to simulate
// something else changing the document in paperless-ngx at that moment.
type hookDocumentProcessor struct {
	text   string
	during func(documentID int)
}

func (p *hookDocumentProcessor) ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions, jobID string) (*ProcessedDocument, error) {
	if p.during != nil {
		p.during(documentID)
	}
	return &ProcessedDocument{ID: documentID, Text: p.text}, nil
}

// TestProcessAutoOcrTagDocuments_KeepsTagChangesMadeDuringOCR is the regression
// test for tags being reverted when auto OCR finishes: the documents are
// fetched once per batch, and the completion update used to write back that
// snapshot's tag list (minus the OCR tag), reverting every tag change a user,
// workflow or script had made in the meantime.
func TestProcessAutoOcrTagDocuments_KeepsTagChangesMadeDuringOCR(t *testing.T) {
	prevAutoOcrTag, prevComplete, prevFailTag := autoOcrTag, pdfOCRCompleteTag, failTag
	t.Cleanup(func() {
		autoOcrTag, pdfOCRCompleteTag, failTag = prevAutoOcrTag, prevComplete, prevFailTag
	})
	autoOcrTag = "paperless-gpt-ocr-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"
	failTag = ""

	for _, tagging := range []bool{true, false} {
		t.Run("pdfOCRTagging="+strconv.FormatBool(tagging), func(t *testing.T) {
			env := newTestEnv(t)
			defer env.teardown()

			fake := &fakePaperless{
				tags: map[string]int{
					"inbox":           19,
					autoOcrTag:        26,
					pdfOCRCompleteTag: 30,
					"finance":         45,
					"tax":             12,
				},
				docID: 4797,
				// 77 is a tag the API user cannot see (not in the catalog).
				docTags: []int{19, 26, 77, 12},
				content: "old text",
			}
			fake.register(env)

			app := &App{
				Client:   env.client,
				Database: newOCRRunTestDB(t),
				docProcessor: &hookDocumentProcessor{
					text: "OCR text",
					during: func(int) {
						fake.addTag("finance")
						fake.removeTag("tax")
					},
				},
				pdfOCRTagging:     tagging,
				pdfOCRCompleteTag: pdfOCRCompleteTag,
			}

			count, err := app.processAutoOcrTagDocuments(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, count)

			want := []string{"", "finance", "inbox"} // "" is the invisible tag 77
			if tagging {
				want = append(want, pdfOCRCompleteTag)
			}
			slices.Sort(want)
			assert.Equal(t, want, fake.docTagNames(),
				"tag changes made while OCR ran must survive, the OCR tag must be gone")
			assert.Equal(t, "OCR text", fake.content)

			// The history row starts from the tags the document had when
			// the change was applied, so undoing it does not revert the
			// concurrent changes either.
			mods, err := GetAllModifications(app.Database)
			require.NoError(t, err)
			var tagMod *ModificationHistory
			for i := range mods {
				if mods[i].ModField == "tags" {
					tagMod = &mods[i]
				}
			}
			require.NotNil(t, tagMod)
			assert.Contains(t, tagMod.PreviousValue, `"finance"`)
			assert.NotContains(t, tagMod.PreviousValue, `"tax"`)
			assert.Contains(t, tagMod.NewValue, `"finance"`)
			assert.NotContains(t, tagMod.NewValue, autoOcrTag)
		})
	}
}

// newDeltaTestFake returns a fake paperless-ngx with one document (ID 7)
// carrying "inbox" and the OCR trigger tag.
func newDeltaTestFake(env *testEnv) *fakePaperless {
	fake := &fakePaperless{
		tags: map[string]int{
			"inbox":                      19,
			"paperless-gpt-ocr-auto":     26,
			"paperless-gpt-ocr-complete": 30,
			"finance":                    45,
		},
		docID:   7,
		docTags: []int{19, 26},
		content: "old text",
	}
	fake.register(env)
	return fake
}

func deltaSuggestion(content string) DocumentSuggestion {
	return DocumentSuggestion{
		ID: 7,
		OriginalDocument: Document{
			ID:      7,
			Tags:    []string{"inbox", "paperless-gpt-ocr-auto"},
			TagIDs:  []int{19, 26},
			Content: "old text",
		},
		SuggestedContent: content,
		RemoveTags:       []string{"paperless-gpt-ocr-auto"},
		AddTags:          []string{"paperless-gpt-ocr-complete"},
		TagsAsDelta:      true,
	}
}

func TestUpdateDocuments_TagsAsDelta(t *testing.T) {
	prevCreate := createNewTags
	t.Cleanup(func() { createNewTags = prevCreate })
	createNewTags = false

	t.Run("fields are PATCHed without tags, then tags change via modify_tags", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		db := newOCRRunTestDB(t)

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{deltaSuggestion("new text")}, db, false))

		assert.Equal(t, []string{"PATCH", "bulk_edit"}, fake.ops)
		require.Len(t, fake.patches, 1)
		assert.NotContains(t, fake.patches[0], "tags", "a delta suggestion must never send a tag list")
		assert.Equal(t, "new text", fake.patches[0]["content"])
		require.Len(t, fake.bulk, 1)
		assert.Equal(t, map[string]any{
			"documents":  []any{float64(7)},
			"method":     "modify_tags",
			"parameters": map[string]any{"add_tags": []any{float64(30)}, "remove_tags": []any{float64(26)}},
		}, fake.bulk[0])
		assert.Equal(t, []string{"inbox", "paperless-gpt-ocr-complete"}, fake.docTagNames())

		mods, err := GetAllModifications(db)
		require.NoError(t, err)
		var tagMods []ModificationHistory
		for _, m := range mods {
			if m.ModField == "tags" {
				tagMods = append(tagMods, m)
			}
		}
		require.Len(t, tagMods, 1)
		assert.Equal(t, `["inbox","paperless-gpt-ocr-auto"]`, tagMods[0].PreviousValue)
		assert.Equal(t, `["inbox","paperless-gpt-ocr-complete"]`, tagMods[0].NewValue)
	})

	t.Run("no field changes: only modify_tags, never a tag-list PATCH", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{deltaSuggestion("old text")}, env.db, false))

		assert.Equal(t, []string{"bulk_edit"}, fake.ops)
		assert.Equal(t, []string{"inbox", "paperless-gpt-ocr-complete"}, fake.docTagNames())
	})

	t.Run("remove-only delta sends an empty add list, not null", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		s := deltaSuggestion("old text")
		s.AddTags = nil

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{s}, env.db, false))

		require.Len(t, fake.bulk, 1)
		assert.Equal(t, map[string]any{"add_tags": []any{}, "remove_tags": []any{float64(26)}}, fake.bulk[0]["parameters"])
		assert.Equal(t, []string{"inbox"}, fake.docTagNames())
	})

	t.Run("precedence and de-duplication match the full-list path", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		s := deltaSuggestion("old text")
		// A suggested tag that is also removed is removed; an AddTag that is
		// also removed is added (same tag configured as trigger and
		// completion tag). Case variants resolve to one ID.
		s.SuggestedTags = []string{"inbox", "Finance", "finance"}
		s.RemoveTags = []string{"INBOX", "paperless-gpt-ocr-auto", "paperless-gpt-ocr-complete", "does-not-exist"}
		s.AddTags = []string{"paperless-gpt-ocr-complete", "unknown-tag"}

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{s}, env.db, false))

		require.Len(t, fake.bulk, 1)
		assert.Equal(t, map[string]any{
			"add_tags":    []any{float64(45), float64(30)},
			"remove_tags": []any{float64(19), float64(26)},
		}, fake.bulk[0]["parameters"])
		assert.Equal(t, []string{"finance", "paperless-gpt-ocr-complete"}, fake.docTagNames())
	})

	t.Run("trigger tag duplicated with different case: every variant is removed", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		fake.tags["Paperless-GPT-OCR-Auto"] = 27
		fake.docTags = []int{19, 27}
		s := deltaSuggestion("old text")
		s.OriginalDocument.Tags = []string{"inbox", "Paperless-GPT-OCR-Auto"}
		s.OriginalDocument.TagIDs = []int{19, 27}

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{s}, env.db, false))

		assert.Equal(t, map[string]any{
			"add_tags":    []any{float64(30)},
			"remove_tags": []any{float64(26), float64(27)},
		}, fake.bulk[0]["parameters"])
		assert.Equal(t, []string{"inbox", "paperless-gpt-ocr-complete"}, fake.docTagNames())
	})

	t.Run("removing the document's last tag works", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		fake.docTags = []int{26}
		s := deltaSuggestion("old text")
		s.OriginalDocument.Tags = []string{"paperless-gpt-ocr-auto"}
		s.OriginalDocument.TagIDs = []int{26}
		s.AddTags = nil

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{s}, env.db, false))
		assert.Empty(t, fake.docTagNames())
	})

	t.Run("marker tag nested under the trigger tag cannot bring the trigger back", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		fake.parent = map[int]int{30: 26} // complete tag is a child of the trigger tag
		db := newOCRRunTestDB(t)

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{deltaSuggestion("old text")}, db, false))

		assert.Equal(t, []string{"bulk_edit", "bulk_edit"}, fake.ops)
		assert.Equal(t, map[string]any{"add_tags": []any{}, "remove_tags": []any{float64(26)}}, fake.bulk[1]["parameters"])
		// Like a PATCH: the trigger is gone, and with it its nested marker.
		assert.Equal(t, []string{"inbox"}, fake.docTagNames())

		mods, err := GetAllModifications(db)
		require.NoError(t, err)
		require.Len(t, mods, 1)
		assert.Equal(t, `["inbox"]`, mods[0].NewValue, "history shows the tags the document really ended up with")
	})

	t.Run("nothing to add or remove: no request at all", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		s := deltaSuggestion("old text")
		s.RemoveTags = []string{"does-not-exist"}
		s.AddTags = nil

		require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{s}, env.db, false))
		assert.Empty(t, fake.ops)
	})

	t.Run("modify_tags failure is returned after the fields were written", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		fake.bulkStatus = http.StatusInternalServerError
		db := newOCRRunTestDB(t)

		err := env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{deltaSuggestion("new text")}, db, false)
		require.Error(t, err)
		assert.Equal(t, []string{"PATCH", "bulk_edit"}, fake.ops)
		assert.Equal(t, "new text", fake.content)
		assert.Equal(t, []string{"inbox", "paperless-gpt-ocr-auto"}, fake.docTagNames(), "the trigger tag stays when the tag change failed")

		mods, err := GetAllModifications(db)
		require.NoError(t, err)
		var fields []string
		for _, m := range mods {
			fields = append(fields, m.ModField)
		}
		assert.Equal(t, []string{"content"}, fields, "the written content stays undoable; no tag change is recorded")
	})

	t.Run("PATCH failure leaves the tags alone", func(t *testing.T) {
		env := newTestEnv(t)
		defer env.teardown()
		fake := newDeltaTestFake(env)
		fake.patchStatus = http.StatusInternalServerError

		err := env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{deltaSuggestion("new text")}, env.db, false)
		require.Error(t, err)
		assert.Equal(t, []string{"PATCH"}, fake.ops, "tags must not change when the fields could not be written")
		assert.Equal(t, []string{"inbox", "paperless-gpt-ocr-auto"}, fake.docTagNames())
	})
}

// The other auto-OCR writes that build on the batch snapshot use the delta
// path too: dropping the OCR tag from a document that is already complete,
// and taking a document out of the queue after repeated failures.
func TestAutoOcrTagWritesUseTagDelta(t *testing.T) {
	prevAutoOcrTag, prevFailTag := autoOcrTag, failTag
	t.Cleanup(func() { autoOcrTag, failTag = prevAutoOcrTag, prevFailTag })
	autoOcrTag = "paperless-gpt-ocr-auto"

	t.Run("already complete", func(t *testing.T) {
		doc := Document{ID: 5, Tags: []string{autoOcrTag, "paperless-gpt-ocr-complete"}}
		client := &recordingClient{taggedDocuments: map[string][]Document{autoOcrTag: {doc}}}
		app := &App{Client: client, Database: newOCRRunTestDB(t), pdfOCRTagging: true, pdfOCRCompleteTag: "paperless-gpt-ocr-complete"}

		_, err := app.processAutoOcrTagDocuments(context.Background())
		require.NoError(t, err)
		require.Len(t, client.calls, 1)
		assert.True(t, client.calls[0].TagsAsDelta)
		assert.Equal(t, []string{autoOcrTag}, client.calls[0].RemoveTags)
	})

	t.Run("markProcessingFailed", func(t *testing.T) {
		failTag = "paperless-gpt-failed"
		client := &recordingClient{}
		require.NoError(t, markProcessingFailed(context.Background(), client, nil, Document{ID: 6}, autoOcrTag, "OCR failed 3 times"))
		require.Len(t, client.calls, 1)
		assert.True(t, client.calls[0].TagsAsDelta)
	})
}

// hookLLM is a mockLLM that runs during() once, on the first LLM call, to
// simulate something else changing the document in paperless-ngx while
// suggestions are generated.
type hookLLM struct {
	mockLLM
	once   sync.Once
	during func()
}

func (m *hookLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.during != nil {
		m.once.Do(m.during)
	}
	return m.mockLLM.GenerateContent(ctx, messages, options...)
}

// TestProcessTagDocuments_KeepsTagChangesMadeDuringGeneration is the
// regression test for the auto-tag and workflow loop reverting tags: the
// suggestion's tag list is built from the document as it was fetched before
// the LLM ran, and used to be written back as the document's full tag list,
// undoing every tag change made in paperless-ngx in the meantime.
func TestProcessTagDocuments_KeepsTagChangesMadeDuringGeneration(t *testing.T) {
	prevAutoTag, prevManualTag, prevAutoOcrTag, prevFailTag := autoTag, manualTag, autoOcrTag, failTag
	prevTagTmpl, prevTitleTmpl := tagTemplate, titleTemplate
	prevGenTitle, prevGenTags, prevGenCorr, prevGenDate, prevGenType := autoGenerateTitle, autoGenerateTags, autoGenerateCorrespondents, autoGenerateCreatedDate, autoGenerateDocumentType
	t.Cleanup(func() {
		autoTag, manualTag, autoOcrTag, failTag = prevAutoTag, prevManualTag, prevAutoOcrTag, prevFailTag
		tagTemplate, titleTemplate = prevTagTmpl, prevTitleTmpl
		autoGenerateTitle, autoGenerateTags, autoGenerateCorrespondents, autoGenerateCreatedDate, autoGenerateDocumentType = prevGenTitle, prevGenTags, prevGenCorr, prevGenDate, prevGenType
	})
	autoTag, manualTag, autoOcrTag, failTag = "paperless-gpt-auto", "paperless-gpt", "paperless-gpt-ocr-auto", ""
	tagTemplate = template.Must(template.New("tag").Parse("{{.Content}}"))
	titleTemplate = template.Must(template.New("title").Parse("{{.Content}}"))
	autoGenerateTitle, autoGenerateTags = "true", "true"
	autoGenerateCorrespondents, autoGenerateCreatedDate, autoGenerateDocumentType = "false", "false", "false"

	const trigger, done = "invoices-in", "invoices-done"
	on, off := true, false
	workflow := WorkflowConfig{
		ID: "invoices", Name: "Invoices", TriggerTag: trigger, CompletionTag: done,
		GenerateTitles: &on, GenerateTags: &on, GenerateCorrespondents: &off,
		GenerateCreatedDate: &off, GenerateDocumentTypes: &off, GenerateCustomFields: &off,
	}

	newFake := func(env *testEnv, triggerTag string) *fakePaperless {
		fake := &fakePaperless{
			tags: map[string]int{
				"inbox":                      19,
				triggerTag:                   26,
				done:                         30,
				"paperless-gpt-ocr-complete": 31,
				"finance":                    45,
				"tax":                        12,
				"invoice":                    50,
				manualTag:                    60,
			},
			docID: 4797,
			// 77 is a tag the API user cannot see (not in the catalog).
			docTags: []int{19, 26, 77, 12},
			content: "old text",
		}
		fake.register(env)
		return fake
	}
	// Both changes happen while the LLM generates the suggestions.
	changeTags := func(fake *fakePaperless) func() {
		return func() {
			fake.addTag("finance")
			fake.removeTag("tax")
		}
	}

	t.Run("AUTO_TAG", func(t *testing.T) {
		isolateWorkflowSettings(t)
		env := newTestEnv(t)
		defer env.teardown()
		fake := newFake(env, autoTag)
		app := &App{Client: env.client, Database: env.db, autoTagComplete: done}
		app.LLM = &hookLLM{mockLLM: mockLLM{Response: "invoice"}, during: changeTags(fake)}

		count, err := app.processAutoTagDocuments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, []string{"", "finance", "inbox", "invoice", done}, fake.docTagNames(),
			"tag changes made while suggestions were generated must survive")
	})

	t.Run("workflow", func(t *testing.T) {
		isolateWorkflowSettings(t)
		useWorkflows(t, workflow)
		env := newTestEnv(t)
		defer env.teardown()
		fake := newFake(env, trigger)
		app := &App{Client: env.client, Database: env.db}
		app.LLM = &hookLLM{mockLLM: mockLLM{Response: "invoice"}, during: changeTags(fake)}

		count, err := app.processAutoTagDocuments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, []string{"", "finance", "inbox", "invoice", done}, fake.docTagNames())

		// The history row starts from the tags the document had when the
		// change was applied, so undoing it does not revert the concurrent
		// changes either.
		mods, err := GetAllModifications(env.db)
		require.NoError(t, err)
		var tagMod *ModificationHistory
		for i := range mods {
			if mods[i].ModField == "tags" {
				tagMod = &mods[i]
			}
		}
		require.NotNil(t, tagMod)
		assert.Contains(t, tagMod.PreviousValue, `"finance"`)
		assert.NotContains(t, tagMod.PreviousValue, `"tax"`)
		assert.Contains(t, tagMod.NewValue, `"invoice"`)
		assert.NotContains(t, tagMod.NewValue, trigger)
	})

	t.Run("workflow with OCR", func(t *testing.T) {
		isolateWorkflowSettings(t)
		wf := workflow
		wf.EnableOCR = &on
		useWorkflows(t, wf)
		env := newTestEnv(t)
		defer env.teardown()
		fake := newFake(env, trigger)
		app := &App{
			Client:            env.client,
			Database:          env.db,
			ocrProvider:       &mockOCRProvider{},
			pdfOCRTagging:     true,
			pdfOCRCompleteTag: "paperless-gpt-ocr-complete",
			// One change while OCR runs, one while the LLM runs.
			docProcessor: &hookDocumentProcessor{text: "OCR text", during: func(int) { fake.addTag("finance") }},
		}
		app.LLM = &hookLLM{mockLLM: mockLLM{Response: "invoice"}, during: func() { fake.removeTag("tax") }}

		count, err := app.processAutoTagDocuments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, []string{"", "finance", "inbox", "invoice", done, "paperless-gpt-ocr-complete"}, fake.docTagNames())
		assert.Equal(t, "OCR text", fake.content)
	})

	t.Run("workflow waiting for approval", func(t *testing.T) {
		isolateWorkflowSettings(t)
		wf := workflow
		wf.RequireApproval = true
		useWorkflows(t, wf)
		env := newTestEnv(t)
		defer env.teardown()
		require.NoError(t, env.db.AutoMigrate(&PendingReview{}))
		fake := newFake(env, trigger)
		app := &App{Client: env.client, Database: env.db}
		app.LLM = &hookLLM{mockLLM: mockLLM{Response: "invoice"}, during: changeTags(fake)}

		count, err := app.processAutoTagDocuments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, []string{"", "finance", "inbox", manualTag}, fake.docTagNames(),
			"moving the document to the review queue must not revert tag changes either")
	})
}
