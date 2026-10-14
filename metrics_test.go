package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestPaperlessServerLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://paperless:8000", "paperless:8000"},
		{"https://paperless.example.com", "paperless.example.com"},
		{"https://paperless.example.com:443", "paperless.example.com"},
		{"http://paperless.example.com:80", "paperless.example.com"},
		{"http://user:pass@paperless:8000/api/", "paperless:8000"},
		{"http://[2001:db8::1]:8000/", "[2001:db8::1]:8000"},
		{"http://[2001:db8::1]/", "2001:db8::1"},
		{"https://paperless.example.com:8443/path?x=1#frag", "paperless.example.com:8443"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, paperlessServerLabel(tt.in), "input %q", tt.in)
	}
}

func TestPaperlessOperation(t *testing.T) {
	tests := []struct {
		method, path, want string
	}{
		{"GET", "api/documents/42/", opGetDocument},
		{"PATCH", "api/documents/42/", opUpdateDocument},
		{"GET", "api/documents/42/download/", opDownload},
		{"GET", "api/documents/42/thumb/", opOther},
		{"POST", "api/documents/post_document/", opUploadDocument},
		{"GET", "api/tags/", opListTags},
		{"POST", "api/tags/", opCreateTag},
		{"GET", "api/correspondents/", opListCorrespondents},
		{"POST", "api/correspondents/", opCreateCorrespondent},
		{"GET", "api/document_types/", opListDocumentTypes},
		{"GET", "api/custom_fields/", opListCustomFields},
		{"GET", "api/documents/?query=invoice", opSearchDocuments},
		{"GET", "api/tasks/?task_id=abc", opGetTask},
		{"DELETE", "api/documents/42/", opDeleteDocument},
		{"GET", "api/unknown/", opOther},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, paperlessOperation(tt.method, tt.path), "%s %s", tt.method, tt.path)
	}
}

func TestPaperlessDoHTTPErrorIsResultError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":["invalid"]}`))
	}))
	t.Cleanup(server.Close)

	client := NewPaperlessClient(server.URL, "token")
	beforeErr := histogramCount(t, paperlessRequestDuration.WithLabelValues("PATCH", opUpdateDocument, resultError))
	beforeOK := histogramCount(t, paperlessRequestDuration.WithLabelValues("PATCH", opUpdateDocument, resultSuccess))
	beforeErrTotal := testutil.ToFloat64(paperlessRequestsTotal.WithLabelValues("PATCH", opUpdateDocument, resultError))

	resp, err := client.Do(context.Background(), "PATCH", "api/documents/1/", nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	assert.Equal(t, beforeErr+1, histogramCount(t, paperlessRequestDuration.WithLabelValues("PATCH", opUpdateDocument, resultError)))
	assert.Equal(t, beforeOK, histogramCount(t, paperlessRequestDuration.WithLabelValues("PATCH", opUpdateDocument, resultSuccess)))
	assert.Equal(t, beforeErrTotal+1, testutil.ToFloat64(paperlessRequestsTotal.WithLabelValues("PATCH", opUpdateDocument, resultError)))
}

func TestPaperlessDoCanceledContext(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewPaperlessClient(server.URL, "token")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	before := histogramCount(t, paperlessRequestDuration.WithLabelValues("GET", opGetDocument, resultCanceled))
	_, err := client.Do(ctx, "GET", "api/documents/1/", nil)
	require.Error(t, err)
	assert.Equal(t, before+1, histogramCount(t, paperlessRequestDuration.WithLabelValues("GET", opGetDocument, resultCanceled)))
}

func TestRecordPDFResultTable(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		wantCreatedInc uint64
	}{
		{"local only", pdfActionLocal, 1},
		{"local plus attached primary", pdfActionAttached, 1},
		{"replaced", pdfActionReplaced, 1},
		{"upload failed", pdfActionFailed, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beforeOutcome := testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(tt.action))
			beforeCreated := histogramCount(t, ocrPDFCreatedDuration.WithLabelValues(tt.action))
			beforeCreatedTotal := testutil.ToFloat64(ocrPDFCreatedTotal.WithLabelValues(tt.action))
			beforeLocal := testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionLocal))
			beforeNone := testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionNone))

			// Single outcome from PDFAction; created histogram only for create actions.
			observePDFOutcome(tt.action)
			recordPDFResult(tt.action, time.Now().Add(-10*time.Millisecond))

			assert.Equal(t, beforeOutcome+1, testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(tt.action)))
			assert.Equal(t, beforeCreated+tt.wantCreatedInc, histogramCount(t, ocrPDFCreatedDuration.WithLabelValues(tt.action)))
			assert.Equal(t, beforeCreatedTotal+float64(tt.wantCreatedInc), testutil.ToFloat64(ocrPDFCreatedTotal.WithLabelValues(tt.action)))
			if tt.action != pdfActionLocal {
				assert.Equal(t, beforeLocal, testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionLocal)))
			}
			if tt.action != pdfActionNone {
				assert.Equal(t, beforeNone, testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionNone)))
			}
		})
	}
}

func TestOCRSkipExistingSetsPDFActionNone(t *testing.T) {
	doc := &ProcessedDocument{ID: 1, Text: "x", PDFAction: "none"}
	before := testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionNone))
	observePDFOutcome(doc.PDFAction)
	assert.Equal(t, before+1, testutil.ToFloat64(ocrPDFOutcomesTotal.WithLabelValues(pdfActionNone)))
}

func TestOCRJobCompletionHistogramOnce(t *testing.T) {
	id := fmt.Sprintf("completion-%d", time.Now().UnixNano())
	jobStore.addJob(&Job{ID: id, Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	before := histogramCount(t, ocrJobsCompletedDuration.WithLabelValues(resultSuccess))
	beforeTotal := testutil.ToFloat64(ocrJobsCompletedTotal.WithLabelValues(resultSuccess))
	jobStore.updateJobStatus(id, "completed", "ok")
	assert.Equal(t, before+1, histogramCount(t, ocrJobsCompletedDuration.WithLabelValues(resultSuccess)))
	assert.Equal(t, beforeTotal+1, testutil.ToFloat64(ocrJobsCompletedTotal.WithLabelValues(resultSuccess)))

	jobStore.updateJobStatus(id, "completed", "ok again")
	assert.Equal(t, before+1, histogramCount(t, ocrJobsCompletedDuration.WithLabelValues(resultSuccess)),
		"duplicate terminal write must not re-observe completion")
	assert.Equal(t, beforeTotal+1, testutil.ToFloat64(ocrJobsCompletedTotal.WithLabelValues(resultSuccess)))
}

func TestOCRJobCancelMapsToCanceled(t *testing.T) {
	id := fmt.Sprintf("cancel-%d", time.Now().UnixNano())
	jobStore.addJob(&Job{ID: id, Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	before := histogramCount(t, ocrJobsCompletedDuration.WithLabelValues(resultCanceled))
	jobStore.updateJobStatus(id, "cancelled", "bye")
	assert.Equal(t, before+1, histogramCount(t, ocrJobsCompletedDuration.WithLabelValues(resultCanceled)))
	assert.GreaterOrEqual(t, testutil.ToFloat64(ocrJobs.WithLabelValues(jobStatusCanceled)), float64(1))
}

func TestSuggestionCancelPendingObservesCanceled(t *testing.T) {
	id := fmt.Sprintf("sug-cancel-%d", time.Now().UnixNano())
	suggestionJobStore.addJob(&SuggestionJob{ID: id, Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	before := histogramCount(t, suggestionJobsCompletedDuration.WithLabelValues(resultCanceled))
	require.True(t, suggestionJobStore.cancelPending(id))
	assert.Equal(t, before+1, histogramCount(t, suggestionJobsCompletedDuration.WithLabelValues(resultCanceled)))
	assert.GreaterOrEqual(t, testutil.ToFloat64(suggestionJobs.WithLabelValues(jobStatusCanceled)), float64(1))
}

func TestJobGaugeEviction(t *testing.T) {
	// Isolate: fill past maxOCRJobs with terminal jobs, then add one more.
	prefix := fmt.Sprintf("evict-%d-", time.Now().UnixNano())
	for i := 0; i < maxOCRJobs+5; i++ {
		jobStore.addJob(&Job{
			ID:        fmt.Sprintf("%s%d", prefix, i),
			Status:    "completed",
			CreatedAt: time.Now().Add(-time.Duration(i) * time.Second),
			UpdatedAt: time.Now().Add(-time.Duration(i) * time.Second),
		})
	}
	jobStore.RLock()
	storeLen := len(jobStore.jobs)
	recount := countOCRJobStatusesLocked()
	jobStore.RUnlock()

	assert.LessOrEqual(t, storeLen, maxOCRJobs)
	for status, n := range recount {
		assert.Equal(t, n, testutil.ToFloat64(ocrJobs.WithLabelValues(status)), "gauge mismatch for %s", status)
	}
	totalGauges := float64(0)
	for _, status := range []string{jobStatusPending, jobStatusInProgress, jobStatusCompleted, jobStatusFailed, jobStatusCanceled} {
		totalGauges += testutil.ToFloat64(ocrJobs.WithLabelValues(status))
	}
	assert.Equal(t, float64(storeLen), totalGauges)
}

func TestHTTPUnmatchedPathAndMetricsSlashExcluded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(metricsMiddleware())
	registerMetricsEndpoint(router)
	router.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	beforeUnmatched := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", httpPathUnmatched, "404"))
	req := httptest.NewRequest(http.MethodGet, "/no-such-route", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, beforeUnmatched+1, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", httpPathUnmatched, "404")))

	beforeSlash := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics/", "404"))
	beforeMetrics := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200"))
	req2 := httptest.NewRequest(http.MethodGet, "/metrics/", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, beforeSlash, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics/", "404")))
	assert.Equal(t, beforeMetrics, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200")))
}

func TestResultFromContext(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Equal(t, resultCanceled, resultFromContext(canceled, errors.New("wrapped")))

	deadline, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	assert.Equal(t, resultError, resultFromContext(deadline, context.DeadlineExceeded))
	assert.Equal(t, resultSuccess, resultFromContext(context.Background(), nil))
}

func TestMetricsEndpointHTTPInstrumentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(metricsMiddleware())
	registerMetricsEndpoint(router)
	router.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "test"})
	})

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/version", "200"))
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, before+1, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/version", "200")))
	assert.GreaterOrEqual(t, histogramCount(t, httpRequestDuration.WithLabelValues("GET", "/api/version")), uint64(1))

	metricsBefore := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200"))
	req3 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
	assert.Equal(t, metricsBefore, testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200")))
	assert.GreaterOrEqual(t, testutil.CollectAndCount(ocrJobsCreatedTotal), 0)
	assert.NotContains(t, w3.Body.String(), "jobs_seconds_total")
}

func TestLLMObserveIncrementsHistogram(t *testing.T) {
	mock := &rateLimitMockLLM{
		generateResponses: []*llms.ContentResponse{
			{Choices: []*llms.ContentChoice{{Content: "ok"}}},
		},
	}
	llm := NewRateLimitedLLM(mock, RateLimitConfig{MaxRetries: 1, Purpose: llmPurposeMetadata})
	before := histogramCount(t, llmRequestDuration.WithLabelValues(string(llmPurposeMetadata), resultSuccess))
	beforeTotal := testutil.ToFloat64(llmRequestsTotal.WithLabelValues(string(llmPurposeMetadata), resultSuccess))
	_, err := llm.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, before+1, histogramCount(t, llmRequestDuration.WithLabelValues(string(llmPurposeMetadata), resultSuccess)))
	assert.Equal(t, beforeTotal+1, testutil.ToFloat64(llmRequestsTotal.WithLabelValues(string(llmPurposeMetadata), resultSuccess)))
}

func TestObserveTimedMovesCounterAndHistogramTogether(t *testing.T) {
	start := time.Now().Add(-5 * time.Millisecond)
	cases := []struct {
		name    string
		observe func()
		hist    prometheus.Observer
		counter prometheus.Counter
	}{
		{
			name:    "paperless",
			observe: func() { observePaperlessRequest("GET", opGetDocument, resultSuccess, start) },
			hist:    paperlessRequestDuration.WithLabelValues("GET", opGetDocument, resultSuccess),
			counter: paperlessRequestsTotal.WithLabelValues("GET", opGetDocument, resultSuccess),
		},
		{
			name:    "llm",
			observe: func() { observeLLMRequest(llmPurposeVision, resultError, start) },
			hist:    llmRequestDuration.WithLabelValues(string(llmPurposeVision), resultError),
			counter: llmRequestsTotal.WithLabelValues(string(llmPurposeVision), resultError),
		},
		{
			name:    "ocr page",
			observe: func() { observeOCRPage(resultSuccess, start) },
			hist:    ocrPageDuration.WithLabelValues(resultSuccess),
			counter: ocrPagesTotal.WithLabelValues(resultSuccess),
		},
		{
			name:    "ocr document",
			observe: func() { observeOCRDocument(resultCanceled, start) },
			hist:    ocrDocumentDuration.WithLabelValues(resultCanceled),
			counter: ocrDocumentsTotal.WithLabelValues(resultCanceled),
		},
		{
			name:    "background",
			observe: func() { observeBackgroundDocument(bgKindAutoTag, resultSuccess, start) },
			hist:    backgroundDocumentsProcessedDuration.WithLabelValues(bgKindAutoTag, resultSuccess),
			counter: backgroundDocumentsProcessedTotal.WithLabelValues(bgKindAutoTag, resultSuccess),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			beforeHist := histogramCount(t, tt.hist)
			beforeTotal := testutil.ToFloat64(tt.counter)
			tt.observe()
			assert.Equal(t, beforeHist+1, histogramCount(t, tt.hist))
			assert.Equal(t, beforeTotal+1, testutil.ToFloat64(tt.counter))
		})
	}
}

func TestObserveLLMTokensKeyStylesAndSkipAbsent(t *testing.T) {
	purpose := llmPurposeMetadata
	promptBefore := testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose)))
	compBefore := testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose)))

	observeLLMTokens(purpose, map[string]any{
		"PromptTokens":     10,
		"CompletionTokens": 4,
	})
	assert.Equal(t, promptBefore+10, testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose))))
	assert.Equal(t, compBefore+4, testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose))))

	promptMid := testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose)))
	compMid := testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose)))
	observeLLMTokens(purpose, map[string]any{
		"InputTokens":  7,
		"OutputTokens": 3,
	})
	assert.Equal(t, promptMid+7, testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose))))
	assert.Equal(t, compMid+3, testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose))))

	promptSkip := testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose)))
	compSkip := testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose)))
	observeLLMTokens(purpose, nil)
	observeLLMTokens(purpose, map[string]any{})
	observeLLMTokens(purpose, map[string]any{"PromptTokens": 0, "CompletionTokens": 0})
	assert.Equal(t, promptSkip, testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(purpose))))
	assert.Equal(t, compSkip, testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(purpose))))
}

func TestLLMGenerateContentRecordsProviderTokens(t *testing.T) {
	mock := &rateLimitMockLLM{
		generateResponses: []*llms.ContentResponse{
			{Choices: []*llms.ContentChoice{{
				Content: "ok",
				GenerationInfo: map[string]any{
					"PromptTokens":     12,
					"CompletionTokens": 5,
				},
			}}},
		},
	}
	llm := NewRateLimitedLLM(mock, RateLimitConfig{MaxRetries: 1, Purpose: llmPurposeVision})
	promptBefore := testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(llmPurposeVision)))
	compBefore := testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(llmPurposeVision)))
	_, err := llm.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, promptBefore+12, testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(llmPurposeVision))))
	assert.Equal(t, compBefore+5, testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(llmPurposeVision))))

	mockNoUsage := &rateLimitMockLLM{
		generateResponses: []*llms.ContentResponse{
			{Choices: []*llms.ContentChoice{{Content: "ok"}}},
		},
	}
	llm2 := NewRateLimitedLLM(mockNoUsage, RateLimitConfig{MaxRetries: 1, Purpose: llmPurposeVision})
	promptMid := testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(llmPurposeVision)))
	compMid := testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(llmPurposeVision)))
	_, err = llm2.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, promptMid, testutil.ToFloat64(llmPromptTokensTotal.WithLabelValues(string(llmPurposeVision))))
	assert.Equal(t, compMid, testutil.ToFloat64(llmCompletionTokensTotal.WithLabelValues(string(llmPurposeVision))))
}

func TestLLMRetryCounter(t *testing.T) {
	seq := &seqMockLLM{
		calls: []seqCall{
			{err: assert.AnError},
			{resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}},
		},
	}
	llm := NewRateLimitedLLM(seq, RateLimitConfig{
		MaxRetries: 3, BackoffMaxWait: 10 * time.Millisecond, Purpose: llmPurposeVision,
	})
	llm.backoffMin = time.Millisecond
	before := testutil.ToFloat64(llmRetriesTotal.WithLabelValues(string(llmPurposeVision)))
	_, err := llm.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, testutil.ToFloat64(llmRetriesTotal.WithLabelValues(string(llmPurposeVision)))-before, float64(1))
}

func TestDocumentFieldEditCounter(t *testing.T) {
	before := testutil.ToFloat64(documentFieldEditsTotal.WithLabelValues(fieldTitle))
	incDocumentFieldEdit(fieldTitle)
	assert.Equal(t, before+1, testutil.ToFloat64(documentFieldEditsTotal.WithLabelValues(fieldTitle)))
}

func TestJobCreatedCountersAndNoIntegralSeries(t *testing.T) {
	ocrBefore := testutil.ToFloat64(ocrJobsCreatedTotal)
	sugBefore := testutil.ToFloat64(suggestionJobsCreatedTotal)
	jobStore.addJob(&Job{ID: fmt.Sprintf("metrics-ocr-%d", time.Now().UnixNano()), Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	suggestionJobStore.addJob(&SuggestionJob{ID: fmt.Sprintf("metrics-sug-%d", time.Now().UnixNano()), Status: "pending", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	assert.Equal(t, ocrBefore+1, testutil.ToFloat64(ocrJobsCreatedTotal))
	assert.Equal(t, sugBefore+1, testutil.ToFloat64(suggestionJobsCreatedTotal))
}

func TestInitMetricsInfo(t *testing.T) {
	InitMetricsInfo("http://paperless:8000", "ollama", "llama3", "llm", "ollama", "llava")
	assert.Equal(t, float64(1), testutil.ToFloat64(buildInfo.WithLabelValues(
		version, commit, "paperless:8000", "ollama", "llama3", "llm", "ollama", "llava",
	)))
}

func histogramCount(t *testing.T, obs prometheus.Observer) uint64 {
	t.Helper()
	h, ok := obs.(prometheus.Histogram)
	require.True(t, ok)
	m := &dto.Metric{}
	require.NoError(t, h.Write(m))
	return m.Histogram.GetSampleCount()
}

type seqCall struct {
	resp *llms.ContentResponse
	err  error
}

type seqMockLLM struct {
	calls []seqCall
	idx   int
}

func (m *seqMockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "", assert.AnError
}

func (m *seqMockLLM) CreateEmbedding(_ context.Context, _ []string) ([][]float32, error) {
	return nil, assert.AnError
}

func (m *seqMockLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	if m.idx >= len(m.calls) {
		return nil, assert.AnError
	}
	c := m.calls[m.idx]
	m.idx++
	return c.resp, c.err
}

func TestResultFromError(t *testing.T) {
	assert.Equal(t, resultSuccess, resultFromError(nil))
	assert.Equal(t, resultCanceled, resultFromError(context.Canceled))
	assert.Equal(t, resultError, resultFromError(assert.AnError))
}
