package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSplitDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sql, err := db.DB()
	require.NoError(t, err)
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { sql.Close() })
	require.NoError(t, db.AutoMigrate(&SplitSuggestion{}))
	return db
}

type splitTestClient struct {
	ClientInterface
	source     splitSource
	data       []byte
	tags       map[string]int
	events     []string
	splitErr   error
	swapErr    error
	cleanupErr error
	list       []int
}

func (c *splitTestClient) InvalidateSplitPreviews(int) error { return nil }

func (c *splitTestClient) GetSplitSource(context.Context, int) (splitSource, error) {
	return c.source, nil
}
func (c *splitTestClient) DownloadPDF(context.Context, Document) ([]byte, error) { return c.data, nil }
func (c *splitTestClient) ListSplitDocumentIDs(context.Context, string) ([]int, error) {
	return c.list, nil
}
func (c *splitTestClient) GetAllTags(context.Context) (map[string]int, error) { return c.tags, nil }
func (c *splitTestClient) EnsureTagExists(context.Context, string) error      { return nil }
func (c *splitTestClient) GetDocument(context.Context, int) (Document, error) {
	return Document{ID: c.source.ID, TagIDs: slices.Clone(c.source.Tags)}, nil
}
func (c *splitTestClient) SetSplitTags(_ context.Context, _ int, tags []int) error {
	c.events = append(c.events, fmt.Sprintf("tags:%v", tags))
	if len(c.events) == 1 && c.swapErr != nil {
		return c.swapErr
	}
	if len(c.events) > 1 && c.cleanupErr != nil {
		return c.cleanupErr
	}
	c.source.Tags = slices.Clone(tags)
	return nil
}
func (c *splitTestClient) SplitDocument(_ context.Context, _ int, _ int, _ []int, deleteOriginals bool) error {
	c.events = append(c.events, fmt.Sprintf("split:delete=%t", deleteOriginals))
	return c.splitErr
}

func splitApplyFixture(t *testing.T) (*App, *splitTestClient, *SplitSuggestion) {
	t.Helper()
	data := []byte("unchanged archive")
	client := &splitTestClient{
		source: splitSource{ID: 17, Title: "Batch", Tags: []int{1, 90}, PageCount: 9, Checksum: "original", MimeType: "application/pdf"},
		data:   data, tags: map[string]int{"split": 1, "auto": 2, "failed": 3},
	}
	app := &App{Client: client, Database: newSplitDB(t), splitConfig: &SplitConfig{Tag: "split", OutputTags: []string{"auto"}, WindowSize: 8}}
	s := &SplitSuggestion{DocumentID: 17, PageCount: 9, Status: "review", SourceChecksum: "original", ArchiveChecksum: fmt.Sprintf("%x", sha256.Sum256(data)), Boundaries: []SplitBoundary{{AfterPage: 3, Confidence: .99, Reason: "x"}}}
	require.NoError(t, app.Database.Create(s).Error)
	return app, client, s
}

func TestSplitApplyTagOrderAndDuplicate(t *testing.T) {
	app, client, s := splitApplyFixture(t)
	result, err := app.applySplitSuggestion(context.Background(), s.ID, []int{3, 4})
	require.NoError(t, err)
	assert.Equal(t, "submitted", result.Status)
	assert.Equal(t, []string{"tags:[90 2]", "split:delete=false", "tags:[90]"}, client.events)
	_, err = app.applySplitSuggestion(context.Background(), s.ID, []int{3, 4})
	require.Error(t, err)
	require.Len(t, client.events, 3)
	var stored SplitSuggestion
	require.NoError(t, app.Database.First(&stored, s.ID).Error)
	assert.Equal(t, []int{3, 4}, stored.AppliedBoundaries)
	assert.Equal(t, []int{1, 90}, stored.OriginalTags)
	assert.Equal(t, []int{2}, stored.OutputTagIDs)
	held, err := app.splitHeld(context.Background(), 17, nil)
	require.NoError(t, err)
	assert.True(t, held)
}

func TestSplitApplyGuards(t *testing.T) {
	for _, kind := range []string{"dry-run", "changed-original", "changed-archive", "invalid-boundaries"} {
		t.Run(kind, func(t *testing.T) {
			app, client, s := splitApplyFixture(t)
			boundaries := []int{3}
			switch kind {
			case "dry-run":
				app.splitConfig.DryRun = true
			case "changed-original":
				client.source.Checksum = "replacement"
			case "changed-archive":
				client.data = []byte("different")
			case "invalid-boundaries":
				boundaries = []int{9}
			}
			_, err := app.applySplitSuggestion(context.Background(), s.ID, boundaries)
			require.Error(t, err)
			assert.Empty(t, client.events)
		})
	}
}

func TestSplitKeepWhole(t *testing.T) {
	for _, pages := range []int{1, 3} {
		for _, status := range []string{"review", "skipped"} {
			t.Run(fmt.Sprintf("%d/%s", pages, status), func(t *testing.T) {
				app, client, s := splitApplyFixture(t)
				s.PageCount, s.Status = pages, status
				client.source.PageCount = pages
				require.NoError(t, app.Database.Save(s).Error)
				router := gin.New()
				router.POST("/split/:id/apply", app.applySplitSuggestionHandler)
				w := httptest.NewRecorder()
				r := httptest.NewRequest("POST", fmt.Sprintf("/split/%d/apply", s.ID), strings.NewReader(`{"boundaries":[]}`))
				r.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(w, r)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.NoError(t, app.Database.First(s, s.ID).Error)
				assert.Equal(t, "kept", s.Status)
				assert.Empty(t, s.AppliedBoundaries)
				assert.Equal(t, []string{"tags:[90 2]"}, client.events)
				held, err := app.splitHeld(context.Background(), s.DocumentID, []string{"auto"})
				require.NoError(t, err)
				assert.False(t, held)
				_, err = app.applySplitSuggestion(context.Background(), s.ID, []int{})
				require.Error(t, err)
				assert.Len(t, client.events, 1)
			})
		}
	}
}

func TestSplitKeepWholeGuards(t *testing.T) {
	for _, kind := range []string{"dry-run", "changed-original", "changed-archive", "tag-failure", "submitted", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			app, client, s := splitApplyFixture(t)
			switch kind {
			case "dry-run":
				app.splitConfig.DryRun = true
			case "changed-original":
				client.source.Checksum = "replacement"
			case "changed-archive":
				client.data = []byte("different")
			case "tag-failure":
				client.swapErr = errors.New("tag update failed")
			default:
				s.Status = kind
				require.NoError(t, app.Database.Save(s).Error)
			}
			_, err := app.applySplitSuggestion(context.Background(), s.ID, []int{})
			require.Error(t, err)
			held, err := app.splitHeld(context.Background(), s.DocumentID, nil)
			require.NoError(t, err)
			assert.True(t, held)
			for _, event := range client.events {
				assert.NotContains(t, event, "split:")
			}
		})
	}
}

func TestSplitApplyFailureRecovery(t *testing.T) {
	for _, stage := range []string{"swap", "submit", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			app, client, s := splitApplyFixture(t)
			switch stage {
			case "swap":
				client.swapErr = errors.New("swap failed")
			case "submit":
				client.splitErr = errors.New("connection lost")
			case "cleanup":
				client.cleanupErr = errors.New("cleanup failed")
			}
			result, err := app.applySplitSuggestion(context.Background(), s.ID, []int{3})
			require.Error(t, err)
			if stage == "swap" {
				assert.Equal(t, "review", result.Status)
				assert.Equal(t, []int{90, 1}, client.source.Tags)
				for _, event := range client.events {
					assert.NotContains(t, event, "split:")
				}
			} else {
				assert.Equal(t, "uncertain", result.Status)
				_, err = app.applySplitSuggestion(context.Background(), s.ID, []int{3})
				require.Error(t, err)
			}
		})
	}
}

func TestSplitInterruptedState(t *testing.T) {
	db := newSplitDB(t)
	require.NoError(t, db.Create(&SplitSuggestion{DocumentID: 1, Status: "generating"}).Error)
	require.NoError(t, db.Create(&SplitSuggestion{DocumentID: 2, Status: "applying"}).Error)
	require.NoError(t, markInterruptedSplits(db))
	var records []SplitSuggestion
	require.NoError(t, db.Order("document_id").Find(&records).Error)
	assert.Equal(t, "failed", records[0].Status)
	assert.Equal(t, "uncertain", records[1].Status)
}

func TestSplitAPIRequest(t *testing.T) {
	for _, response := range []string{`{"result":"OK"}`, `{"result":"FAILED"}`, `invalid`} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/documents/bulk_edit/", r.URL.Path)
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "Token test", r.Header.Get("Authorization"))
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.JSONEq(t, `{"documents":[17],"method":"split","parameters":{"pages":"1-3,4,5-9","delete_originals":false}}`, string(body))
				fmt.Fprint(w, response)
			}))
			defer server.Close()
			client := NewPaperlessClient(server.URL, "test")
			err := client.SplitDocument(context.Background(), 17, 9, []int{3, 4}, false)
			if strings.Contains(response, "OK") {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSplitAPIPaginationAndTagVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			fmt.Fprint(w, `{"tags":[1]}`)
			return
		}
		assert.Equal(t, "split", r.URL.Query().Get("tags__name__iexact"))
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `{"next":"https://untrusted.example/next","results":[{"id":1}]}`)
		} else {
			fmt.Fprint(w, `{"next":null,"results":[{"id":2}]}`)
		}
	}))
	defer server.Close()
	client := NewPaperlessClient(server.URL, "test")
	ids, err := client.ListSplitDocumentIDs(context.Background(), "split")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, ids)
	require.Error(t, client.SetSplitTags(context.Background(), 1, []int{2}))
}

type splitTestLLM struct {
	llms.Model
	calls     int
	responses []string
	err       error
}

func (m *splitTestLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	m.calls++
	if m.err != nil {
		return "", m.err
	}
	return m.responses[min(m.calls-1, len(m.responses)-1)], nil
}

func TestSplitGenerationRetryAndPersistence(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			app, client, old := splitApplyFixture(t)
			require.NoError(t, app.Database.Delete(old).Error)
			client.data = syntheticSplitPDF(6)
			client.source.PageCount = 6
			model := &splitTestLLM{responses: []string{"bad JSON", `[{"after_page":2,"confidence":0.99,"reason":"New sender"},{"after_page":4,"confidence":0.98,"reason":"New date"}]`}}
			if failed {
				model.responses = []string{"bad JSON"}
			}
			app.LLM = model
			app.splitConfig.DryRun = true
			previousTag := failTag
			failTag = "failed"
			t.Cleanup(func() { failTag = previousTag })
			previousTemplate := splitTemplate
			t.Cleanup(func() { splitTemplate = previousTemplate })
			prompt, err := os.ReadFile("default_prompts/split_prompt.tmpl")
			require.NoError(t, err)
			splitTemplate, err = template.New("split").Parse(string(prompt))
			require.NoError(t, err)
			suggestion, err := app.generateSplitSuggestion(context.Background(), 17, false)
			require.Equal(t, 2, model.calls)
			if failed {
				require.Error(t, err)
				assert.Equal(t, "failed", suggestion.Status)
				assert.Contains(t, client.source.Tags, 3)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "review", suggestion.Status)
				assert.Len(t, suggestion.Boundaries, 2)
				assert.Empty(t, client.events)
			}
			_, err = app.generateSplitSuggestion(context.Background(), 17, false)
			require.NoError(t, err)
			assert.Equal(t, 2, model.calls)
		})
	}
}

func TestSplitAutomaticBackground(t *testing.T) {
	previousTemplate := splitTemplate
	t.Cleanup(func() { splitTemplate = previousTemplate })
	splitTemplate = template.Must(template.New("split").Parse("{{.Pages}}"))
	for _, kind := range []string{"eligible", "low-confidence", "pagination", "conflict", "dry-run", "manual", "empty", "submit-failure", "swap-failure", "existing", "regenerate"} {
		t.Run(kind, func(t *testing.T) {
			app, client, old := splitApplyFixture(t)
			if kind != "existing" && kind != "regenerate" {
				require.NoError(t, app.Database.Delete(old).Error)
			}
			client.data, client.source.PageCount, client.list = syntheticSplitPDF(6), 6, []int{17}
			app.splitConfig.Auto, app.splitConfig.ConfidenceThreshold = true, .9
			model := &splitTestLLM{responses: []string{`[{"after_page":2,"confidence":0.99,"reason":"sender"},{"after_page":4,"confidence":0.9,"reason":"sender"}]`}}
			app.LLM = model
			switch kind {
			case "low-confidence":
				model.responses = []string{`[{"after_page":2,"confidence":0.99,"reason":"sender"},{"after_page":4,"confidence":0.89,"reason":"sender"}]`}
			case "pagination":
				model.responses = []string{`[{"after_page":1,"confidence":0.99,"reason":"sender"},{"after_page":4,"confidence":0.99,"reason":"sender"}]`}
			case "conflict":
				app.splitConfig.WindowSize = 5
				model.responses = []string{model.responses[0], `[]`}
			case "dry-run":
				app.splitConfig.DryRun = true
			case "manual":
				app.splitConfig.Auto = false
			case "empty":
				model.responses = []string{`[]`}
			case "submit-failure":
				client.splitErr = errors.New("connection lost")
			case "swap-failure":
				client.swapErr = errors.New("tag swap failed")
			}
			if kind == "regenerate" {
				_, err := app.generateSplitSuggestion(context.Background(), 17, true)
				require.NoError(t, err)
			} else {
				_, err := app.processSplitTagDocuments(context.Background())
				if kind == "submit-failure" || kind == "swap-failure" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			}
			var stored SplitSuggestion
			require.NoError(t, app.Database.Where("document_id = ?", 17).First(&stored).Error)
			switch kind {
			case "eligible":
				assert.Equal(t, "submitted", stored.Status)
				assert.Equal(t, []int{2, 4}, stored.AppliedBoundaries)
				assert.Equal(t, []string{"tags:[90 2]", "split:delete=false", "tags:[90]"}, client.events)
			case "submit-failure":
				assert.Equal(t, "uncertain", stored.Status)
			case "swap-failure":
				assert.Equal(t, "review", stored.Status)
				assert.NotEmpty(t, stored.Error)
			default:
				assert.Equal(t, "review", stored.Status)
				assert.Empty(t, client.events)
			}
			calls, events := model.calls, len(client.events)
			_, err := app.processSplitTagDocuments(context.Background())
			require.NoError(t, err)
			assert.Equal(t, calls, model.calls)
			assert.Len(t, client.events, events, "existing Suggestions must never retry automatically")
		})
	}
}

// Synthetic text only, no user documents. Each two-page pair is one letter.
func syntheticSplitPDF(pages int) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	kids := []string{}
	for page := 1; page <= pages; page++ {
		objectID := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", objectID))
		content := fmt.Sprintf("BT /F1 18 Tf 50 750 Td (Sender %d - Page %d of 2) Tj ET", (page+1)/2, (page-1)%2+1)
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", objectID+1), fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return b.Bytes()
}

func TestSplitConcurrentApplyHandlers(t *testing.T) {
	app, client, s := splitApplyFixture(t)
	router := gin.New()
	router.POST("/split/:id/apply", app.applySplitSuggestionHandler)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", fmt.Sprintf("/split/%d/apply", s.ID), strings.NewReader(`{"boundaries":[3]}`))
			r.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, r)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	got := []int{}
	for code := range codes {
		got = append(got, code)
	}
	slices.Sort(got)
	assert.Equal(t, []int{200, 409}, got)
	assert.Len(t, client.events, 3)
}

type splitReviewContext struct {
	context.Context
	betweenDocuments func()
}

func (ctx splitReviewContext) Err() error {
	ctx.betweenDocuments()
	return ctx.Context.Err()
}

func TestSplitReleasesLockBetweenQueuedDocuments(t *testing.T) {
	app, client, suggestion := splitApplyFixture(t)
	client.data = syntheticSplitPDF(2)
	client.source.PageCount = 2
	client.list = []int{18, 19}
	model := &splitTestLLM{responses: []string{`[]`}}
	app.LLM = model
	previousTemplate := splitTemplate
	t.Cleanup(func() { splitTemplate = previousTemplate })
	splitTemplate = template.Must(template.New("split").Parse("{{.Pages}}"))
	reviewAvailable := false
	ctx := splitReviewContext{Context: context.Background(), betweenDocuments: func() {
		// Probe between queue items without relying on mutex waiter scheduling.
		if !app.splitMu.TryLock() {
			return
		}
		if model.calls == 1 {
			reviewAvailable = true
		}
		app.splitMu.Unlock()
	}}
	count, err := app.processSplitTagDocuments(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, model.calls)
	app.splitMu.Lock()
	available := reviewAvailable
	app.splitMu.Unlock()
	require.True(t, available, "Review must be available before processing the next queued document")
	router := gin.New()
	router.POST("/split/:id/skip", app.skipSplitSuggestionHandler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", fmt.Sprintf("/split/%d/skip", suggestion.ID), nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, app.Database.First(suggestion, suggestion.ID).Error)
	assert.Equal(t, "skipped", suggestion.Status)
}

func TestSplitListAndSkip(t *testing.T) {
	app, _, s := splitApplyFixture(t)
	router := gin.New()
	router.GET("/split", app.listSplitSuggestionsHandler)
	router.POST("/split/:id/skip", app.skipSplitSuggestionHandler)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/split", nil))
	require.Equal(t, 200, w.Code)
	var response struct {
		Count   int               `json:"count"`
		Results []SplitSuggestion `json:"results"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, 1, response.Count)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/split/%d/skip", s.ID), nil))
	require.Equal(t, 200, w.Code)
	require.NoError(t, app.Database.First(s, s.ID).Error)
	assert.Equal(t, "skipped", s.Status)
}

func TestSplitProcessingExcludesBeforeLimit(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name__iexact") != "" {
			fmt.Fprint(w, `{"count":1,"results":[{"document_count":30}]}`)
			return
		}
		fmt.Fprint(w, `{"results":[{"id":1,"name":"split"},{"id":2,"name":"auto"}]}`)
	})
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("tags__id__none"))
		assert.Equal(t, "25", r.URL.Query().Get("page_size"))
		assert.Equal(t, "id", r.URL.Query().Get("ordering"))
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"count":1,"results":[{"id":30,"tags":[2]}]}`)
	})
	app := &App{Client: env.client, Database: newSplitDB(t), splitConfig: &SplitConfig{Tag: "split"}}
	docs, err := app.processingDocuments(context.Background(), "auto")
	require.NoError(t, err)
	require.Len(t, docs, 1)
	assert.Equal(t, 30, docs[0].ID)
	docs, err = env.client.GetDocumentsByTagExcluding(context.Background(), "auto", 25, "split", 2)
	require.NoError(t, err)
	assert.Empty(t, docs)
}

func TestSplitHundredPageBatchHasNoWindowEdgeSplits(t *testing.T) {
	windows := splitWindows(105, 8)
	results := make([]splitWindowResult, 0, len(windows))
	seen := make(map[int]bool)
	for _, window := range windows {
		assert.LessOrEqual(t, window.Last-window.First+1, 8)
		for boundary := window.First; boundary < window.Last; boundary++ {
			seen[boundary] = true
		}
		results = append(results, splitWindowResult{Window: window})
	}
	assert.Len(t, seen, 104, "every adjacent page pair must be considered")
	assert.Empty(t, mergeSplitWindows(results), "window edges never imply boundaries")
}

func TestSplitSourceRejectsUnsupportedOrIncompleteMetadata(t *testing.T) {
	for _, source := range []string{`{"id":1,"page_count":3}`, `{"id":1,"page_count":0}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/metadata/") {
				fmt.Fprint(w, `{"original_checksum":"abc","original_mime_type":"image/png"}`)
			} else {
				fmt.Fprint(w, source)
			}
		}))
		client := NewPaperlessClient(server.URL, "test")
		_, err := client.GetSplitSource(context.Background(), 1)
		require.Error(t, err)
		server.Close()
	}
}

func TestSplitBackgroundSkipsPersistedAndTaggedSources(t *testing.T) {
	app, _, _ := splitApplyFixture(t)
	held, err := app.splitHeld(context.Background(), 17, []string{"auto"})
	require.NoError(t, err)
	assert.True(t, held)
	held, err = app.splitHeld(context.Background(), 18, []string{"SPLIT"})
	require.NoError(t, err)
	assert.True(t, held)
	held, err = app.splitHeld(context.Background(), 19, []string{"auto"})
	require.NoError(t, err)
	assert.False(t, held)
}

func TestSplitBackgroundHoldByStatus(t *testing.T) {
	for _, status := range []string{"generating", "review", "skipped", "applying", "submitted", "uncertain", "failed", "kept"} {
		t.Run(status, func(t *testing.T) {
			app, _, suggestion := splitApplyFixture(t)
			require.NoError(t, app.Database.Model(suggestion).Update("status", status).Error)
			held, err := app.splitHeld(context.Background(), suggestion.DocumentID, []string{"auto"})
			require.NoError(t, err)
			assert.Equal(t, status != "failed" && status != "kept", held)
			held, err = app.splitHeld(context.Background(), suggestion.DocumentID, []string{"SPLIT"})
			require.NoError(t, err)
			assert.True(t, held, "the split tag must hold every status")
		})
	}
}

func TestSplitRegenerationInvalidatesOnlyPagePreviews(t *testing.T) {
	client := NewPaperlessClient("http://unused", "test")
	client.CacheFolder = t.TempDir()
	directory := filepath.Join(client.CacheFolder, "document-17")
	require.NoError(t, os.MkdirAll(directory, 0700))
	for _, name := range []string{"preview-page000.jpg", "preview-page001.jpg", "ocr-page000.jpg"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("cached"), 0600))
	}
	require.NoError(t, client.InvalidateSplitPreviews(17))
	_, err := os.Stat(filepath.Join(directory, "preview-page000.jpg"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(directory, "preview-page001.jpg"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(directory, "ocr-page000.jpg"))
	require.NoError(t, err)
	require.NoError(t, client.InvalidateSplitPreviews(999))
}

type splitPollingClient struct {
	ClientInterface
	pages []int
}

func (c *splitPollingClient) GetDocumentsByTagExcluding(_ context.Context, _ string, size int, _ string, page int) ([]Document, error) {
	c.pages = append(c.pages, page)
	if page == 2 {
		return []Document{{ID: 26}}, nil
	}
	documents := make([]Document, size)
	for i := range documents {
		documents[i].ID = i + 1
	}
	return documents, nil
}

func TestSplitRetainedOriginalsDoNotStarveQueue(t *testing.T) {
	client := &splitPollingClient{}
	app := &App{Client: client, Database: newSplitDB(t), splitConfig: &SplitConfig{Tag: "split"}}
	for id := 1; id <= 25; id++ {
		require.NoError(t, app.Database.Create(&SplitSuggestion{DocumentID: id, Status: "submitted"}).Error)
	}
	documents, err := app.processingDocuments(context.Background(), "auto")
	require.NoError(t, err)
	require.Len(t, documents, 1)
	assert.Equal(t, 26, documents[0].ID)
	assert.Equal(t, []int{1, 2}, client.pages)
}
