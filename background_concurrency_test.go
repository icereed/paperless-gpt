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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	"paperless-gpt/internal/pdfrender"
	"paperless-gpt/ocr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// The tests in this file change package globals (autoOCRWorkers, tags,
// retry limits), so none of them may run in parallel with other tests.

// setAutoOCRGlobals sets the globals the auto-OCR poll reads and restores
// them when the test ends.
func setAutoOCRGlobals(t *testing.T, workers int) {
	t.Helper()
	prevWorkers, prevAutoOcrTag, prevCompleteTag, prevFailTag := autoOCRWorkers, autoOcrTag, pdfOCRCompleteTag, failTag
	prevMaxRetries, prevLimit, prevCreateNewTags := ocrMaxRetries, limitOcrPages, createNewTags
	t.Cleanup(func() {
		autoOCRWorkers, autoOcrTag, pdfOCRCompleteTag, failTag = prevWorkers, prevAutoOcrTag, prevCompleteTag, prevFailTag
		ocrMaxRetries, limitOcrPages, createNewTags = prevMaxRetries, prevLimit, prevCreateNewTags
	})
	autoOCRWorkers = workers
	autoOcrTag = "paperless-gpt-ocr-auto"
	pdfOCRCompleteTag = "paperless-gpt-ocr-complete"
	failTag = "paperless-gpt-failed"
	ocrMaxRetries = 3
	createNewTags = false
}

// newFileTestDB opens a file-backed SQLite database the same way
// InitializeDB does. An in-memory database would give every pooled
// connection its own empty database once workers write concurrently.
func newFileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ModificationHistory{}, &OCRPageResult{}, &OCRRun{}, &PendingReview{}))
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	})
	return db
}

// concurrentClient is a ClientInterface double that is safe for parallel
// workers. Methods the auto-OCR poll does not use fall through to the nil
// embedded client and would panic.
type concurrentClient struct {
	*PaperlessClient
	documents  []Document
	updateErrs map[int]error // returned by UpdateDocuments for a document

	mu         sync.Mutex
	updates    []DocumentSuggestion
	tags       map[string]int
	createdTag []string
}

func (c *concurrentClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	return c.documents, nil
}

func (c *concurrentClient) GetDocument(ctx context.Context, documentID int) (Document, error) {
	for _, d := range c.documents {
		if d.ID == documentID {
			return d, nil
		}
	}
	return Document{}, fmt.Errorf("document %d not found", documentID)
}

func (c *concurrentClient) UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates = append(c.updates, documents...)
	for _, d := range documents {
		if err := c.updateErrs[d.ID]; err != nil {
			return err
		}
	}
	return nil
}

func (c *concurrentClient) GetAllTags(ctx context.Context) (map[string]int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tags := make(map[string]int, len(c.tags))
	for name, id := range c.tags {
		tags[name] = id
	}
	return tags, nil
}

func (c *concurrentClient) CreateTag(ctx context.Context, tagName string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tags == nil {
		c.tags = map[string]int{}
	}
	c.createdTag = append(c.createdTag, tagName)
	c.tags[tagName] = len(c.tags) + 100
	return c.tags[tagName], nil
}

func (c *concurrentClient) updatesFor(documentID int) []DocumentSuggestion {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []DocumentSuggestion
	for _, u := range c.updates {
		if u.ID == documentID {
			out = append(out, u)
		}
	}
	return out
}

// gatedProcessor is a DocumentProcessor double that records how many
// documents are being OCRed at the same time. With a gate, every call
// blocks until the gate is closed.
type gatedProcessor struct {
	gate      chan struct{} // nil: don't block
	started   chan int      // receives the document ID when a call starts
	perDocErr map[int]error
	nilDocs   map[int]bool // documents for which OCR is "skipped" (nil result)

	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	calls       atomic.Int32

	mu    sync.Mutex
	order []int
}

func newGatedProcessor(gated bool) *gatedProcessor {
	p := &gatedProcessor{started: make(chan int, 100)}
	if gated {
		p.gate = make(chan struct{})
	}
	return p
}

func (p *gatedProcessor) ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions, jobID string) (*ProcessedDocument, error) {
	p.calls.Add(1)
	n := p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	for {
		m := p.maxInFlight.Load()
		if n <= m || p.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	p.mu.Lock()
	p.order = append(p.order, documentID)
	p.mu.Unlock()
	p.started <- documentID
	if p.gate != nil {
		<-p.gate
	}
	if err := p.perDocErr[documentID]; err != nil {
		return nil, err
	}
	if p.nilDocs[documentID] {
		return nil, nil
	}
	return &ProcessedDocument{ID: documentID, Text: fmt.Sprintf("text of document %d", documentID)}, nil
}

func autoOCRDocs(ids ...int) []Document {
	docs := make([]Document, 0, len(ids))
	for _, id := range ids {
		docs = append(docs, Document{ID: id, Title: fmt.Sprintf("Doc %d", id), Tags: []string{"paperless-gpt-ocr-auto"}})
	}
	return docs
}

type autoOCRResult struct {
	count int
	err   error
}

// runAutoOCR runs one auto-OCR poll in the background so a test can observe
// workers while they are blocked.
func runAutoOCR(ctx context.Context, app *App) <-chan autoOCRResult {
	done := make(chan autoOCRResult, 1)
	go func() {
		count, err := app.processAutoOcrTagDocuments(ctx)
		done <- autoOCRResult{count, err}
	}()
	return done
}

func waitStarted(t *testing.T, p *gatedProcessor, n int) []int {
	t.Helper()
	var ids []int
	for len(ids) < n {
		select {
		case id := <-p.started:
			ids = append(ids, id)
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d documents started", len(ids), n)
		}
	}
	return ids
}

func waitDone(t *testing.T, done <-chan autoOCRResult) autoOCRResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("auto-OCR poll did not return")
		return autoOCRResult{}
	}
}

func TestAutoOCRWorkers_ProcessesUpToTheLimitAtTheSameTime(t *testing.T) {
	setAutoOCRGlobals(t, 3)
	client := &concurrentClient{documents: autoOCRDocs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)}
	processor := newGatedProcessor(true)
	app := &App{Client: client, docProcessor: processor, Database: newFileTestDB(t)}

	done := runAutoOCR(context.Background(), app)

	// Three documents are in flight at the same time...
	waitStarted(t, processor, 3)
	// ...and no fourth starts while all three workers are busy.
	select {
	case id := <-processor.started:
		t.Fatalf("document %d started while 3 workers were busy", id)
	case <-time.After(150 * time.Millisecond):
	}
	assert.Equal(t, int32(3), processor.inFlight.Load())

	close(processor.gate)
	result := waitDone(t, done)

	require.NoError(t, result.err)
	assert.Equal(t, 10, result.count)
	assert.Equal(t, int32(10), processor.calls.Load())
	assert.Equal(t, int32(3), processor.maxInFlight.Load(), "never more than AUTO_OCR_WORKERS documents at once")
	for id := 1; id <= 10; id++ {
		updates := client.updatesFor(id)
		require.Len(t, updates, 1, "document %d", id)
		assert.Equal(t, fmt.Sprintf("text of document %d", id), updates[0].SuggestedContent)
		assert.Equal(t, []string{autoOcrTag}, updates[0].RemoveTags)
	}
}

func TestAutoOCRWorkers_OneWorkerKeepsDocumentOrder(t *testing.T) {
	setAutoOCRGlobals(t, 1)
	client := &concurrentClient{documents: autoOCRDocs(5, 3, 9, 1)}
	processor := newGatedProcessor(false)
	app := &App{Client: client, docProcessor: processor, Database: newFileTestDB(t)}

	count, err := app.processAutoOcrTagDocuments(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 4, count)
	assert.Equal(t, int32(1), processor.maxInFlight.Load())
	assert.Equal(t, []int{5, 3, 9, 1}, processor.order)
	assert.Empty(t, client.createdTag, "one worker must not add the per-batch tag check")
}

func TestAutoOCRWorkers_InvalidGlobalFallsBackToOne(t *testing.T) {
	for _, workers := range []int{0, -3} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			setAutoOCRGlobals(t, workers)
			client := &concurrentClient{documents: autoOCRDocs(1, 2, 3)}
			processor := newGatedProcessor(false)
			app := &App{Client: client, docProcessor: processor, Database: newFileTestDB(t)}

			result := waitDone(t, runAutoOCR(context.Background(), app))

			require.NoError(t, result.err)
			assert.Equal(t, 3, result.count)
			assert.Equal(t, int32(1), processor.maxInFlight.Load())
		})
	}
}

func TestAutoOCRWorkers_MoreWorkersThanDocuments(t *testing.T) {
	setAutoOCRGlobals(t, 25)
	client := &concurrentClient{documents: autoOCRDocs(1, 2)}
	processor := newGatedProcessor(false)
	app := &App{Client: client, docProcessor: processor, Database: newFileTestDB(t)}

	count, err := app.processAutoOcrTagDocuments(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestAutoOCRWorkers_OutcomesMatchSequential runs one batch that hits every
// outcome of processAutoOcrDocument and checks that one worker and four
// workers report the same count and the same errors, in document order.
func TestAutoOCRWorkers_OutcomesMatchSequential(t *testing.T) {
	run := func(t *testing.T, workers int) (int, error, *concurrentClient) {
		setAutoOCRGlobals(t, workers)
		ocrMaxRetries = 2

		docs := autoOCRDocs(1, 2, 3, 4, 5, 6, 7)
		docs[3].Tags = append(docs[3].Tags, pdfOCRCompleteTag) // 4: already OCRed
		client := &concurrentClient{
			documents: docs,
			tags:      map[string]int{autoOcrTag: 1, pdfOCRCompleteTag: 2, failTag: 3},
			updateErrs: map[int]error{
				6: &PartialUpdateError{DocumentID: 6, DroppedFields: []string{"created_date"}},
				7: errors.New("paperless-ngx rejected the update"),
			},
		}
		processor := newGatedProcessor(false)
		processor.perDocErr = map[int]error{
			2: errors.New("provider error"),       // counted, retried next poll
			3: errors.New("provider error again"), // reaches OCR_MAX_RETRIES
		}
		processor.nilDocs = map[int]bool{5: true}
		app := &App{
			Client:            client,
			docProcessor:      processor,
			Database:          newFileTestDB(t),
			pdfOCRTagging:     true,
			pdfOCRCompleteTag: pdfOCRCompleteTag,
		}
		app.ocrFailures.recordFailure(3) // one earlier failed poll

		count, err := app.processAutoOcrTagDocuments(context.Background())
		return count, err, client
	}

	seqCount, seqErr, _ := run(t, 1)
	parCount, parErr, client := run(t, 4)

	// 1 success, 4 already complete, 6 partial update; 2, 3, 5, 7 are not counted.
	assert.Equal(t, 3, seqCount)
	assert.Equal(t, seqCount, parCount)
	require.Error(t, seqErr)
	require.Error(t, parErr)
	assert.Equal(t, seqErr.Error(), parErr.Error(), "same errors in the same order")
	assert.Regexp(t, regexp.MustCompile(`(?s)document 2 OCR error: provider error.*document 7 update error`), parErr.Error())
	assert.NotContains(t, parErr.Error(), "document 3", "a fail-tagged document must not keep the loop in backoff")

	// Document 3 was taken out of the queue with the fail tag.
	doc3 := client.updatesFor(3)
	require.Len(t, doc3, 1)
	assert.Equal(t, []string{autoOcrTag}, doc3[0].RemoveTags)
	assert.Equal(t, []string{failTag}, doc3[0].SuggestedTags)
	// Document 4 only lost the auto OCR tag.
	doc4 := client.updatesFor(4)
	require.Len(t, doc4, 1)
	assert.Empty(t, doc4[0].SuggestedContent)
	// Document 5 was skipped without an update.
	assert.Empty(t, client.updatesFor(5))
	// Document 7: failed update, then the recovery PATCH.
	assert.Len(t, client.updatesFor(7), 2)
}

func TestAutoOCRWorkers_CancelWhileWaitingForAWorker(t *testing.T) {
	setAutoOCRGlobals(t, 2)
	client := &concurrentClient{documents: autoOCRDocs(1, 2, 3, 4, 5)}
	processor := newGatedProcessor(true)
	db := newFileTestDB(t)
	app := &App{Client: client, docProcessor: processor, Database: db}

	ctx, cancel := context.WithCancel(context.Background())
	done := runAutoOCR(ctx, app)

	started := waitStarted(t, processor, 2)
	// The dispatcher is now blocked waiting for a free worker for document 3.
	cancel()
	close(processor.gate)
	result := waitDone(t, done)

	assert.Equal(t, int32(2), processor.calls.Load(), "no document may start after the cancellation")
	var runs int64
	require.NoError(t, db.Model(&OCRRun{}).Count(&runs).Error)
	assert.Equal(t, int64(2), runs, "documents that never started get no OCR run")
	// The two started documents finished normally; the rest were left
	// untouched for the next poll after a restart.
	assert.Equal(t, 2, result.count)
	assert.NoError(t, result.err)
	for _, id := range started {
		assert.Len(t, client.updatesFor(id), 1)
	}
}

func TestAutoOCRWorkers_CreatesMissingTagsOnceBeforeFanOut(t *testing.T) {
	setAutoOCRGlobals(t, 3)
	client := &concurrentClient{
		documents: autoOCRDocs(1, 2, 3, 4),
		tags:      map[string]int{"paperless-gpt-ocr-auto": 1},
	}
	app := &App{
		Client:            client,
		docProcessor:      newGatedProcessor(false),
		Database:          newFileTestDB(t),
		pdfOCRTagging:     true,
		pdfOCRCompleteTag: pdfOCRCompleteTag,
	}

	_, err := app.processAutoOcrTagDocuments(context.Background())

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{pdfOCRCompleteTag, failTag}, client.createdTag)
}

func TestParseAutoOCRWorkers(t *testing.T) {
	for raw, want := range map[string]int{"": 1, "1": 1, "4": 4, "25": 25} {
		got, err := parseAutoOCRWorkers(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"0", "-1", "abc", "2.5", " 3"} {
		_, err := parseAutoOCRWorkers(raw)
		assert.Error(t, err, raw)
	}
}

func TestOCRProviderForDocument(t *testing.T) {
	shared := &ocr.GoogleDocAIProvider{}
	first := ocrProviderForDocument(shared)
	second := ocrProviderForDocument(shared)
	assert.IsType(t, &ocr.GoogleDocAIProvider{}, first)
	assert.NotSame(t, shared, first, "Document AI collects hOCR per document and needs its own copy")
	assert.NotSame(t, first, second)

	other := &mockOCRProvider{text: "x"}
	assert.Same(t, other, ocrProviderForDocument(other), "stateless providers are used as they are")
}

// TestPruneOCRRunsConcurrently checks that parallel workers pruning at the
// same time keep exactly the record cap and never evict a run in progress.
func TestPruneOCRRunsConcurrently(t *testing.T) {
	db := newFileTestDB(t)
	base := time.Now().Add(-time.Hour)

	active := OCRRun{JobID: "active", DocumentID: 1, Trigger: "auto", Status: "in_progress", StartedAt: base}
	require.NoError(t, db.Create(&active).Error)
	require.NoError(t, db.Create(&OCRPageResult{DocumentID: 1, JobID: "active", PageIndex: 0, Text: "page"}).Error)

	runs := make([]OCRRun, 0, maxOCRRunRecords+24)
	for i := 1; i < maxOCRRunRecords+25; i++ {
		runs = append(runs, OCRRun{
			JobID:      fmt.Sprintf("job-%d", i),
			DocumentID: 1000 + i,
			Trigger:    "auto",
			Status:     "completed",
			StartedAt:  base.Add(time.Duration(i) * time.Second),
		})
	}
	require.NoError(t, db.CreateInBatches(runs, 200).Error)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(documentID int) {
			defer wg.Done()
			assert.NoError(t, PruneOCRRuns(db, documentID))
		}(2000 + i)
	}
	wg.Wait()

	var total int64
	require.NoError(t, db.Model(&OCRRun{}).Count(&total).Error)
	assert.Equal(t, int64(maxOCRRunRecords), total)
	var stillActive int64
	require.NoError(t, db.Model(&OCRRun{}).Where("job_id = ?", "active").Count(&stillActive).Error)
	assert.Equal(t, int64(1), stillActive, "a run in progress must never be pruned")
	var activePages int64
	require.NoError(t, db.Model(&OCRPageResult{}).Where("job_id = ?", "active").Count(&activePages).Error)
	assert.Equal(t, int64(1), activePages)
}

// TestPdfcpuConfigConcurrentFirstUse runs in a fresh process, so pdfcpu's
// default configuration is really loaded for the first time by several
// goroutines at once. Run with -race to detect the unsynchronized load.
func TestPdfcpuConfigConcurrentFirstUse(t *testing.T) {
	if os.Getenv("PAPERLESS_GPT_TEST_PDFCPU_CHILD") == "1" {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conf := pdfcpuConfig()
				conf.Cmd = 0 // every caller owns its copy
			}()
		}
		wg.Wait()
		return
	}
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPdfcpuConfigConcurrentFirstUse$", "-test.count=1")
	cmd.Env = append(os.Environ(), "PAPERLESS_GPT_TEST_PDFCPU_CHILD=1", "HOME="+home, "XDG_CONFIG_HOME="+home)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// recordingOCRServer is a minimal paperless-ngx for the end-to-end test: it
// serves each document's PDF and records PATCHed contents. Handlers run in
// parallel, so all shared state is behind mu.
type recordingOCRServer struct {
	pdfs map[int][]byte

	mu         sync.Mutex
	contents   map[int]string
	unexpected []string
}

func (s *recordingOCRServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/api/tags/":
		_, _ = w.Write([]byte(`{"count":3,"next":null,"results":[{"id":1,"name":"paperless-gpt-ocr-auto"},{"id":2,"name":"paperless-gpt-ocr-complete"},{"id":3,"name":"paperless-gpt-failed"}]}`))
		return
	case r.Method == http.MethodGet && (path == "/api/correspondents/" || path == "/api/document_types/" || path == "/api/custom_fields/"):
		_, _ = w.Write([]byte(`{"count":0,"next":null,"results":[]}`))
		return
	}
	var id int
	if _, err := fmt.Sscanf(path, "/api/documents/%d/", &id); err == nil {
		switch {
		case r.Method == http.MethodGet && path == fmt.Sprintf("/api/documents/%d/download/", id):
			if pdf, ok := s.pdfs[id]; ok {
				_, _ = w.Write(pdf)
				return
			}
		case r.Method == http.MethodPatch && path == fmt.Sprintf("/api/documents/%d/", id):
			body, _ := io.ReadAll(r.Body)
			var patch map[string]any
			_ = json.Unmarshal(body, &patch)
			if content, ok := patch["content"].(string); ok {
				s.mu.Lock()
				s.contents[id] = content
				s.mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
			return
		}
	}
	s.mu.Lock()
	s.unexpected = append(s.unexpected, r.Method+" "+r.URL.String())
	s.mu.Unlock()
	http.Error(w, "unexpected request", http.StatusInternalServerError)
}

// byteHashOCRProvider answers with a digest of the page it is given, so the
// recognized text identifies exactly which page was OCRed. PDF input is
// rendered first, because split PDFs carry fresh IDs and timestamps on every
// run. Each call takes a moment so that documents really overlap.
type byteHashOCRProvider struct {
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
}

func (p *byteHashOCRProvider) ProcessImage(ctx context.Context, data []byte, pageNumber int) (*ocr.OCRResult, error) {
	n := p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	for {
		m := p.maxInFlight.Load()
		if n <= m || p.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	if bytes.HasPrefix(data, []byte("%PDF")) {
		doc, err := pdfrender.Open(ctx, data)
		if err != nil {
			return nil, err
		}
		defer doc.Close()
		img, err := doc.RenderDPI(0, 20)
		if err != nil {
			return nil, err
		}
		return &ocr.OCRResult{Text: fmt.Sprintf("page %d of %d %x", pageNumber, doc.NumPages(), sha256.Sum256(img.Pix))}, nil
	}
	return &ocr.OCRResult{Text: fmt.Sprintf("page %d %x", pageNumber, sha256.Sum256(data))}, nil
}

// TestAutoOCRWorkers_EndToEnd runs the real OCR pipeline (download, PDF
// splitting or rendering, OCR run bookkeeping, page results, the real
// paperless-ngx update with its history rows) with four workers against a
// file-backed database, and checks that every document gets exactly the text
// the same pipeline produces with one worker.
func TestAutoOCRWorkers_EndToEnd(t *testing.T) {
	files := []string{"tests/pdf/five-pager.pdf", "tests/pdf/sample.pdf", "tests/pdf/five-pager.pdf", "tests/pdf/sample.pdf", "tests/pdf/five-pager.pdf", "tests/pdf/sample.pdf"}
	pdfs := map[int][]byte{}
	var ids []int
	for i, f := range files {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		id := 101 + i
		pdfs[id] = data
		ids = append(ids, id)
	}

	cases := []struct {
		name      string
		mode      string
		pageLimit int
	}{
		{"pdf mode, all pages (split)", "pdf", 0},
		{"pdf mode, page limit (trim and split)", "pdf", 2},
		{"image mode (rendering)", "image", 2},
		{"whole pdf mode", "whole_pdf", 0},
	}

	prevTemplate := ocrTemplate
	ocrTemplate = template.Must(template.New("ocr").Parse("Transcribe the page."))
	t.Cleanup(func() { ocrTemplate = prevTemplate })

	run := func(t *testing.T, mode string, pageLimit, workers int) (map[int]string, *gorm.DB, *byteHashOCRProvider) {
		setAutoOCRGlobals(t, workers)
		limitOcrPages = pageLimit
		// A fresh default cache folder: the client's CacheFolder stays unset.
		t.Setenv("TMPDIR", t.TempDir())

		server := &recordingOCRServer{pdfs: pdfs, contents: map[int]string{}}
		httpServer := httptest.NewServer(server)
		t.Cleanup(httpServer.Close)
		client := NewPaperlessClient(httpServer.URL, "test-token")
		client.CacheFolder = ""

		docs := make([]Document, 0, len(ids))
		for _, id := range ids {
			docs = append(docs, Document{ID: id, Title: fmt.Sprintf("Doc %d", id), Tags: []string{autoOcrTag}, TagIDs: []int{1}})
		}
		provider := &byteHashOCRProvider{}
		db := newFileTestDB(t)
		app := &App{
			Client:            &listedDocumentsClient{PaperlessClient: client, documents: docs},
			Database:          db,
			ocrProvider:       provider,
			ocrProcessMode:    mode,
			pdfOCRTagging:     true,
			pdfOCRCompleteTag: pdfOCRCompleteTag,
		}

		count, err := app.processAutoOcrTagDocuments(context.Background())
		require.NoError(t, err)
		assert.Equal(t, len(ids), count)
		server.mu.Lock()
		defer server.mu.Unlock()
		assert.Empty(t, server.unexpected)
		contents := make(map[int]string, len(server.contents))
		for id, c := range server.contents {
			contents[id] = c
		}
		return contents, db, provider
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sequential, _, _ := run(t, tc.mode, tc.pageLimit, 1)
			parallel, db, provider := run(t, tc.mode, tc.pageLimit, 4)

			assert.Greater(t, provider.maxInFlight.Load(), int32(1), "documents must have been OCRed at the same time")
			require.Len(t, parallel, len(ids))
			for _, id := range ids {
				assert.NotEmpty(t, parallel[id], "document %d", id)
				assert.Equal(t, sequential[id], parallel[id], "document %d must get the same text as with one worker", id)
			}

			var runs []OCRRun
			require.NoError(t, db.Order("document_id").Find(&runs).Error)
			require.Len(t, runs, len(ids))
			for _, r := range runs {
				assert.Equal(t, "completed", r.Status, "run of document %d", r.DocumentID)
				var pages int64
				require.NoError(t, db.Model(&OCRPageResult{}).Where("job_id = ? AND document_id = ?", r.JobID, r.DocumentID).Count(&pages).Error)
				assert.Positive(t, pages, "page results of document %d", r.DocumentID)
			}
			var history int64
			require.NoError(t, db.Model(&ModificationHistory{}).Count(&history).Error)
			assert.Positive(t, history, "the updates must be recorded in the history")
		})
	}
}

// listedDocumentsClient is the real PaperlessClient, except that the poll's
// document listing is answered directly.
type listedDocumentsClient struct {
	*PaperlessClient
	documents []Document
}

func (c *listedDocumentsClient) GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error) {
	return c.documents, nil
}
