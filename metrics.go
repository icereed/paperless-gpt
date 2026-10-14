package main

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Label value enums (frozen).
const (
	resultSuccess  = "success"
	resultError    = "error"
	resultCanceled = "canceled"

	jobStatusPending    = "pending"
	jobStatusInProgress = "in_progress"
	jobStatusCompleted  = "completed"
	jobStatusFailed     = "failed"
	jobStatusCanceled   = "canceled"

	opGetDocument         = "get_document"
	opUpdateDocument      = "update_document"
	opDownload            = "download"
	opUploadDocument      = "upload_document"
	opListTags            = "list_tags"
	opSearchDocuments     = "search_documents"
	opGetTask             = "get_task"
	opDeleteDocument      = "delete_document"
	opCreateTag           = "create_tag"
	opCreateCorrespondent = "create_correspondent"
	opListCorrespondents  = "list_correspondents"
	opListDocumentTypes   = "list_document_types"
	opListCustomFields    = "list_custom_fields"
	opOther               = "other"

	fieldTitle         = "title"
	fieldTags          = "tags"
	fieldCorrespondent = "correspondent"
	fieldDocumentType  = "document_type"
	fieldCreatedDate   = "created_date"
	fieldContent       = "content"
	fieldCustomFields  = "custom_fields"

	pdfActionNone     = "none"
	pdfActionAttached = "attached"
	pdfActionReplaced = "replaced"
	pdfActionSkipped  = "skipped"
	pdfActionFailed   = "failed"
	pdfActionLocal    = "local"

	bgKindAutoTag     = "auto_tag"
	bgKindAutoOCR     = "auto_ocr"
	bgKindWorkflowTag = "workflow_tag"

	httpPathUnmatched = "unmatched"
)

var (
	httpBuckets      = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	llmOCRJobBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	ocrDocBuckets    = []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600}
)

var (
	buildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "paperless_gpt_build_info",
		Help: "Build and process configuration identity (always 1).",
	}, []string{"version", "commit", "paperless_server", "llm_backend", "llm_model", "ocr_backend", "vision_backend", "vision_model"})

	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_http_requests_total",
		Help: "Inbound HTTP requests handled by the Gin server.",
	}, []string{"method", "path", "code"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_http_request_duration_seconds",
		Help:    "Inbound HTTP request latency in seconds.",
		Buckets: httpBuckets,
	}, []string{"method", "path"})

	paperlessRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_paperless_requests_total",
		Help: "Outbound paperless-ngx API requests.",
	}, []string{"method", "operation", "result"})

	paperlessRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_paperless_request_duration_seconds",
		Help:    "Outbound paperless-ngx API request latency in seconds.",
		Buckets: httpBuckets,
	}, []string{"method", "operation", "result"})

	llmRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_llm_requests_total",
		Help: "LLM requests (including retries until final outcome).",
	}, []string{"purpose", "result"})

	llmRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_llm_request_duration_seconds",
		Help:    "LLM request latency in seconds (including retries).",
		Buckets: llmOCRJobBuckets,
	}, []string{"purpose", "result"})

	llmRetriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_llm_retries_total",
		Help: "LLM retry attempts after a failed call.",
	}, []string{"purpose"})

	llmPromptTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_llm_prompt_tokens_total",
		Help: "LLM prompt/input tokens reported by the backend (skipped when usage is absent).",
	}, []string{"purpose"})

	llmCompletionTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_llm_completion_tokens_total",
		Help: "LLM completion/output tokens reported by the backend (skipped when usage is absent).",
	}, []string{"purpose"})

	ocrPagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_pages_total",
		Help: "Per-page (or whole-PDF) OCR provider calls.",
	}, []string{"result"})

	ocrPageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_ocr_page_duration_seconds",
		Help:    "Per-page (or whole-PDF) OCR provider call latency in seconds.",
		Buckets: llmOCRJobBuckets,
	}, []string{"result"})

	ocrDocumentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_documents_total",
		Help: "End-to-end OCR document processing attempts.",
	}, []string{"result"})

	ocrDocumentDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_ocr_document_duration_seconds",
		Help:    "End-to-end OCR document processing latency in seconds.",
		Buckets: ocrDocBuckets,
	}, []string{"result"})

	documentFieldEditsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_document_field_edits_total",
		Help: "Successful document field modifications recorded in local history.",
	}, []string{"field"})

	ocrPDFOutcomesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_pdf_outcomes_total",
		Help: "Searchable PDF outcomes from OCR processing.",
	}, []string{"action"})

	ocrPDFCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_pdf_created_total",
		Help: "Successful searchable PDF creates (attached, replaced, or local).",
	}, []string{"action"})

	ocrPDFCreatedDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_ocr_pdf_created_duration_seconds",
		Help:    "Latency of successful searchable PDF creates (attached, replaced, or local).",
		Buckets: llmOCRJobBuckets,
	}, []string{"action"})

	ocrJobs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "paperless_gpt_ocr_jobs",
		Help: "Current OCR jobs by status.",
	}, []string{"status"})

	suggestionJobs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "paperless_gpt_suggestion_jobs",
		Help: "Current suggestion jobs by status.",
	}, []string{"status"})

	ocrJobsCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_jobs_created_total",
		Help: "OCR jobs enqueued (created) since process start.",
	})

	suggestionJobsCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "paperless_gpt_suggestion_jobs_created_total",
		Help: "Suggestion jobs enqueued (created) since process start.",
	})

	ocrJobsCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_ocr_jobs_completed_total",
		Help: "OCR jobs that reached a terminal status.",
	}, []string{"result"})

	ocrJobsCompletedDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_ocr_jobs_completed_duration_seconds",
		Help:    "OCR job lifetime from creation to terminal status.",
		Buckets: llmOCRJobBuckets,
	}, []string{"result"})

	suggestionJobsCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_suggestion_jobs_completed_total",
		Help: "Suggestion jobs that reached a terminal status.",
	}, []string{"result"})

	suggestionJobsCompletedDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_suggestion_jobs_completed_duration_seconds",
		Help:    "Suggestion job lifetime from creation to terminal status.",
		Buckets: llmOCRJobBuckets,
	}, []string{"result"})

	backgroundDocumentsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_background_documents_processed_total",
		Help: "Background poller per-document processing attempts.",
	}, []string{"kind", "result"})

	backgroundDocumentsProcessedDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "paperless_gpt_background_documents_processed_duration_seconds",
		Help:    "Background poller per-document processing latency.",
		Buckets: llmOCRJobBuckets,
	}, []string{"kind", "result"})

	backgroundCycleErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "paperless_gpt_background_cycle_errors_total",
		Help: "Background poller cycle errors.",
	}, []string{"kind"})

	paperlessTagBacklog = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "paperless_gpt_paperless_tag_backlog",
		Help: "Documents on paperless-ngx matching configured processing tags (refreshed each background poll cycle).",
	}, []string{"kind"})
)

func init() {
	registerCollectorOnce(collectors.NewGoCollector())
	registerCollectorOnce(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	for _, status := range []string{
		jobStatusPending, jobStatusInProgress, jobStatusCompleted, jobStatusFailed, jobStatusCanceled,
	} {
		ocrJobs.WithLabelValues(status).Set(0)
		suggestionJobs.WithLabelValues(status).Set(0)
	}
}

func registerCollectorOnce(c prometheus.Collector) {
	if err := prometheus.Register(c); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return
		}
		panic(err)
	}
}

// InitMetricsInfo sets the process-identity gauge from boot config.
func InitMetricsInfo(paperlessBaseURL, llmBackend, llmModel, ocrBackend, visionBackend, visionModel string) {
	buildInfo.Reset()
	buildInfo.WithLabelValues(
		version,
		commit,
		paperlessServerLabel(paperlessBaseURL),
		llmBackend,
		llmModel,
		ocrBackend,
		visionBackend,
		visionModel,
	).Set(1)
}

// paperlessServerLabel returns host[:port] from a paperless base URL for the
// info gauge. Credentials, path, query, and fragment are never included.
func paperlessServerLabel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		return host
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return host
	}
	// IPv6 hosts need brackets when a port is present.
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func observeDuration(hist prometheus.Observer, start time.Time) {
	hist.Observe(time.Since(start).Seconds())
}

// observeTimed increments counter and observes hist with the same labels so they cannot drift.
func observeTimed(hist *prometheus.HistogramVec, counter *prometheus.CounterVec, start time.Time, labelValues ...string) {
	hist.WithLabelValues(labelValues...).Observe(time.Since(start).Seconds())
	counter.WithLabelValues(labelValues...).Inc()
}

func generationInfoFloat(info map[string]any, keys ...string) (float64, bool) {
	if info == nil {
		return 0, false
	}
	for _, key := range keys {
		v, ok := info[key]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case int:
			return float64(n), true
		case int32:
			return float64(n), true
		case int64:
			return float64(n), true
		case uint:
			return float64(n), true
		case uint32:
			return float64(n), true
		case uint64:
			return float64(n), true
		case float32:
			return float64(n), true
		case float64:
			return n, true
		}
	}
	return 0, false
}

// observeLLMTokens adds provider-reported usage from GenerationInfo.
// Supports OpenAI-style PromptTokens/CompletionTokens and Anthropic InputTokens/OutputTokens.
// Skips when usage keys are absent or non-positive.
func observeLLMTokens(purpose llmPurpose, info map[string]any) {
	prompt, promptOK := generationInfoFloat(info, "PromptTokens", "InputTokens")
	completion, completionOK := generationInfoFloat(info, "CompletionTokens", "OutputTokens")
	label := string(purpose)
	if promptOK && prompt > 0 {
		llmPromptTokensTotal.WithLabelValues(label).Add(prompt)
	}
	if completionOK && completion > 0 {
		llmCompletionTokensTotal.WithLabelValues(label).Add(completion)
	}
}

func resultFromError(err error) string {
	if err == nil {
		return resultSuccess
	}
	if errors.Is(err, context.Canceled) {
		return resultCanceled
	}
	return resultError
}

func resultFromContext(ctx context.Context, err error) string {
	if err == nil {
		return resultSuccess
	}
	if ctx != nil && ctx.Err() == context.Canceled {
		return resultCanceled
	}
	return resultFromError(err)
}

func jobStatusMetricLabel(status string) string {
	if status == "cancelled" {
		return jobStatusCanceled
	}
	return status
}

func isTerminalJobStatus(status string) bool {
	switch jobStatusMetricLabel(status) {
	case jobStatusCompleted, jobStatusFailed, jobStatusCanceled:
		return true
	default:
		return false
	}
}

func jobResultFromStatus(status string) string {
	switch jobStatusMetricLabel(status) {
	case jobStatusCompleted:
		return resultSuccess
	case jobStatusCanceled:
		return resultCanceled
	default:
		return resultError
	}
}

func paperlessOperation(method, path string) string {
	pathOnly := path
	if i := strings.Index(path, "?"); i >= 0 {
		pathOnly = path[:i]
	}
	pathOnly = strings.Trim(pathOnly, "/")
	parts := strings.Split(pathOnly, "/")

	switch {
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "tags":
		if method == "POST" {
			return opCreateTag
		}
		return opListTags
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "correspondents":
		if method == "POST" {
			return opCreateCorrespondent
		}
		return opListCorrespondents
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "document_types":
		return opListDocumentTypes
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "custom_fields":
		return opListCustomFields
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "tasks":
		return opGetTask
	case len(parts) >= 2 && parts[0] == "api" && parts[1] == "documents":
		if method == "POST" && strings.Contains(pathOnly, "post_document") {
			return opUploadDocument
		}
		if len(parts) >= 4 && parts[3] == "download" {
			return opDownload
		}
		// Document subresources other than download (e.g. thumb) are unlisted.
		if len(parts) >= 4 && parts[3] != "" {
			return opOther
		}
		if method == "DELETE" {
			return opDeleteDocument
		}
		if method == "PATCH" || method == "PUT" {
			return opUpdateDocument
		}
		if method == "GET" && len(parts) == 3 && parts[2] != "" {
			return opGetDocument
		}
		if method == "GET" {
			return opSearchDocuments
		}
	}
	return opOther
}

func observePaperlessRequest(method, operation, result string, start time.Time) {
	observeTimed(paperlessRequestDuration, paperlessRequestsTotal, start, method, operation, result)
}

func observeLLMRequest(purpose llmPurpose, result string, start time.Time) {
	observeTimed(llmRequestDuration, llmRequestsTotal, start, string(purpose), result)
}

func incLLMRetry(purpose llmPurpose) {
	llmRetriesTotal.WithLabelValues(string(purpose)).Inc()
}

func observeOCRPage(result string, start time.Time) {
	observeTimed(ocrPageDuration, ocrPagesTotal, start, result)
}

func observeOCRDocument(result string, start time.Time) {
	observeTimed(ocrDocumentDuration, ocrDocumentsTotal, start, result)
}

func incDocumentFieldEdit(field string) {
	documentFieldEditsTotal.WithLabelValues(field).Inc()
}

func observePDFOutcome(action string) {
	ocrPDFOutcomesTotal.WithLabelValues(action).Inc()
}

func observePDFCreated(action string, start time.Time) {
	observeTimed(ocrPDFCreatedDuration, ocrPDFCreatedTotal, start, action)
}

// recordPDFResult observes created-duration for attached/replaced/local only.
func recordPDFResult(action string, start time.Time) {
	switch action {
	case pdfActionAttached, pdfActionReplaced, pdfActionLocal:
		observePDFCreated(action, start)
	}
}

func emptyJobStatusCounts() map[string]float64 {
	return map[string]float64{
		jobStatusPending:    0,
		jobStatusInProgress: 0,
		jobStatusCompleted:  0,
		jobStatusFailed:     0,
		jobStatusCanceled:   0,
	}
}

func refreshJobGauges(gauge *prometheus.GaugeVec, counts map[string]float64) {
	for status, n := range counts {
		gauge.WithLabelValues(status).Set(n)
	}
}

func countOCRJobStatusesLocked() map[string]float64 {
	counts := emptyJobStatusCounts()
	for _, job := range jobStore.jobs {
		label := jobStatusMetricLabel(job.Status)
		if _, ok := counts[label]; ok {
			counts[label]++
		}
	}
	return counts
}

func countSuggestionJobStatusesLocked() map[string]float64 {
	counts := emptyJobStatusCounts()
	for _, job := range suggestionJobStore.jobs {
		label := jobStatusMetricLabel(job.Status)
		if _, ok := counts[label]; ok {
			counts[label]++
		}
	}
	return counts
}

func observeOCRJobCompleted(status string, createdAt time.Time) {
	observeTimed(ocrJobsCompletedDuration, ocrJobsCompletedTotal, createdAt, jobResultFromStatus(status))
}

func observeSuggestionJobCompleted(status string, createdAt time.Time) {
	observeTimed(suggestionJobsCompletedDuration, suggestionJobsCompletedTotal, createdAt, jobResultFromStatus(status))
}

func observeBackgroundDocument(kind, result string, start time.Time) {
	observeTimed(backgroundDocumentsProcessedDuration, backgroundDocumentsProcessedTotal, start, kind, result)
}

func incBackgroundCycleError(kind string) {
	backgroundCycleErrorsTotal.WithLabelValues(kind).Inc()
}

func setPaperlessTagBacklog(kind string, count int) {
	paperlessTagBacklog.WithLabelValues(kind).Set(float64(count))
}

// metricsMiddleware records inbound HTTP RED metrics, skipping /metrics.
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSuffix(c.Request.URL.Path, "/") == "/metrics" {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = httpPathUnmatched
		}
		method := c.Request.Method
		code := strconv.Itoa(c.Writer.Status())
		httpRequestsTotal.WithLabelValues(method, path, code).Inc()
		observeDuration(httpRequestDuration.WithLabelValues(method, path), start)
	}
}

// registerMetricsEndpoint exposes GET /metrics on the Gin router.
func registerMetricsEndpoint(router *gin.Engine) {
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
}
