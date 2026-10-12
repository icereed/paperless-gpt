package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, paperlessServerLabel(tt.in), "input %q", tt.in)
	}
}

func TestPaperlessOperation(t *testing.T) {
	assert.Equal(t, opGetDocument, paperlessOperation("GET", "api/documents/42/"))
	assert.Equal(t, opUpdateDocument, paperlessOperation("PATCH", "api/documents/42/"))
	assert.Equal(t, opDownload, paperlessOperation("GET", "api/documents/42/download/"))
	assert.Equal(t, opUploadDocument, paperlessOperation("POST", "api/documents/post_document/"))
	assert.Equal(t, opListTags, paperlessOperation("GET", "api/tags/"))
	assert.Equal(t, opCreateTag, paperlessOperation("POST", "api/tags/"))
	assert.Equal(t, opSearchDocuments, paperlessOperation("GET", "api/documents/?query=invoice"))
	assert.Equal(t, opGetTask, paperlessOperation("GET", "api/tasks/?task_id=abc"))
	assert.Equal(t, opDeleteDocument, paperlessOperation("DELETE", "api/documents/42/"))
	assert.Equal(t, opOther, paperlessOperation("GET", "api/unknown/"))
}

func TestMetricsEndpointHTTPInstrumentation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(metricsMiddleware())
	registerMetricsEndpoint(router)
	router.GET("/api/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": "test"})
	})

	// Hit a real API route so HTTP counter + histogram are observed.
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/version", "200"))

	req2 := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)

	after := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/api/version", "200"))
	assert.GreaterOrEqual(t, after-before, float64(1))

	// Histogram should have observations for the same path.
	metric := &dto.Metric{}
	err := httpRequestDuration.WithLabelValues("GET", "/api/version").(prometheus.Histogram).Write(metric)
	require.NoError(t, err)
	require.NotNil(t, metric.Histogram)
	assert.GreaterOrEqual(t, metric.Histogram.GetSampleCount(), uint64(1))

	// /metrics itself must not be counted by the middleware.
	metricsBefore := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200"))
	req3 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
	body, err := io.ReadAll(w3.Body)
	require.NoError(t, err)
	text := string(body)
	assert.Contains(t, text, "paperless_gpt_http_requests_total")
	assert.Contains(t, text, "paperless_gpt_http_request_duration_seconds")
	assert.NotContains(t, text, "paperless_gpt_ocr_jobs_seconds_total")
	assert.NotContains(t, text, "paperless_gpt_suggestion_jobs_seconds_total")

	metricsAfter := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/metrics", "200"))
	assert.Equal(t, metricsBefore, metricsAfter, "/metrics must be excluded from HTTP self-scrape metrics")
}

func TestLLMObserveIncrementsHistogram(t *testing.T) {
	mock := &rateLimitMockLLM{
		generateResponses: []*llms.ContentResponse{
			{Choices: []*llms.ContentChoice{{Content: "ok"}}},
		},
	}
	llm := NewRateLimitedLLM(mock, RateLimitConfig{
		MaxRetries: 1,
		Purpose:    llmPurposeMetadata,
	})

	before := histogramCount(t, llmRequestDuration.WithLabelValues(llmPurposeMetadata, resultSuccess))
	_, err := llm.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	after := histogramCount(t, llmRequestDuration.WithLabelValues(llmPurposeMetadata, resultSuccess))
	assert.Equal(t, before+1, after)
}

func TestLLMRetryCounter(t *testing.T) {
	seq := &seqMockLLM{
		calls: []seqCall{
			{err: assert.AnError},
			{resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "ok"}}}},
		},
	}
	llm := NewRateLimitedLLM(seq, RateLimitConfig{
		MaxRetries:     3,
		BackoffMaxWait: 10 * time.Millisecond,
		Purpose:        llmPurposeVision,
	})
	llm.backoffMin = time.Millisecond

	before := testutil.ToFloat64(llmRetriesTotal.WithLabelValues(llmPurposeVision))
	_, err := llm.GenerateContent(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: "hi"}}},
	})
	require.NoError(t, err)
	after := testutil.ToFloat64(llmRetriesTotal.WithLabelValues(llmPurposeVision))
	assert.GreaterOrEqual(t, after-before, float64(1))
}

func TestDocumentFieldEditCounter(t *testing.T) {
	before := testutil.ToFloat64(documentFieldEditsTotal.WithLabelValues(fieldTitle))
	incDocumentFieldEdit(fieldTitle)
	after := testutil.ToFloat64(documentFieldEditsTotal.WithLabelValues(fieldTitle))
	assert.Equal(t, before+1, after)
}

func TestJobCreatedCountersAndNoIntegralSeries(t *testing.T) {
	ocrBefore := testutil.ToFloat64(ocrJobsCreatedTotal)
	sugBefore := testutil.ToFloat64(suggestionJobsCreatedTotal)

	jobStore.addJob(&Job{
		ID:        "metrics-ocr-" + time.Now().Format("150405.000"),
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	suggestionJobStore.addJob(&SuggestionJob{
		ID:        "metrics-sug-" + time.Now().Format("150405.000"),
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	assert.Equal(t, ocrBefore+1, testutil.ToFloat64(ocrJobsCreatedTotal))
	assert.Equal(t, sugBefore+1, testutil.ToFloat64(suggestionJobsCreatedTotal))
	assert.GreaterOrEqual(t, testutil.ToFloat64(ocrJobs.WithLabelValues(jobStatusPending)), float64(1))
	assert.GreaterOrEqual(t, testutil.ToFloat64(suggestionJobs.WithLabelValues(jobStatusPending)), float64(1))

	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMetricsEndpoint(router)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "paperless_gpt_ocr_jobs_created_total")
	assert.Contains(t, body, "paperless_gpt_suggestion_jobs_created_total")
	assert.Contains(t, body, "paperless_gpt_ocr_jobs{")
	assert.NotContains(t, body, "jobs_seconds_total")
}

func TestInitMetricsInfo(t *testing.T) {
	InitMetricsInfo("http://paperless:8000", "ollama", "llama3", "llm", "ollama", "llava")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMetricsEndpoint(router)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "paperless_gpt_build_info")
	assert.Contains(t, body, `paperless_server="paperless:8000"`)
	assert.Contains(t, body, `llm_backend="ollama"`)
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
	assert.True(t, strings.HasPrefix(resultCanceled, "cancel"))
}
