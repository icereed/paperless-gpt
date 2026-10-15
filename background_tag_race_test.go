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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		// Same order as paperless-ngx: remove first, then add.
		f.docTags = slices.DeleteFunc(f.docTags, func(id int) bool {
			return slices.Contains(body.Parameters.RemoveTags, id)
		})
		for _, id := range body.Parameters.AddTags {
			if !slices.Contains(f.docTags, id) {
				f.docTags = append(f.docTags, id)
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
		})
	}
}
