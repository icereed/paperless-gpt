package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type blockingLLM struct{}

func (m *blockingLLM) CreateEmbedding(_ context.Context, _ []string) ([][]float32, error) {
	return nil, nil
}

func (m *blockingLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	response, err := m.GenerateContent(ctx, []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextContent{Text: prompt}}},
	}, options...)
	if err != nil {
		return "", err
	}
	return response.Choices[0].Content, nil
}

func (m *blockingLLM) GenerateContent(ctx context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func resetSuggestionJobTestState(t *testing.T) {
	t.Helper()

	oldStore := suggestionJobStore
	oldQueue := suggestionJobQueue
	oldCancellers := suggestionJobCancellers

	suggestionJobStore = &SuggestionJobStore{jobs: make(map[string]*SuggestionJob)}
	suggestionJobQueue = make(chan *SuggestionJob, 100)
	suggestionJobCancellers = make(map[string]context.CancelFunc)

	t.Cleanup(func() {
		suggestionJobStore = oldStore
		suggestionJobQueue = oldQueue
		suggestionJobCancellers = oldCancellers
	})
}

func TestSubmitSuggestionJobHandler(t *testing.T) {
	resetSuggestionJobTestState(t)
	gin.SetMode(gin.TestMode)

	app := &App{}
	router := gin.Default()
	router.POST("/api/jobs/suggestions", app.submitSuggestionJobHandler)

	payload := GenerateSuggestionsRequest{
		Documents: []Document{{ID: 123, Title: "Invoice", Content: "content"}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/api/jobs/suggestions", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var response map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotEmpty(t, response["job_id"])

	job, exists := suggestionJobStore.getJob(response["job_id"])
	require.True(t, exists)
	assert.Equal(t, "pending", job.Status)
	assert.Equal(t, 1, job.TotalDocuments)
}

func TestSubmitSuggestionJobHandlerFailsFastWhenQueueFull(t *testing.T) {
	resetSuggestionJobTestState(t)
	gin.SetMode(gin.TestMode)
	suggestionJobQueue = make(chan *SuggestionJob, 1)
	suggestionJobQueue <- &SuggestionJob{ID: "queued"}

	app := &App{}
	router := gin.Default()
	router.POST("/api/jobs/suggestions", app.submitSuggestionJobHandler)

	payload := GenerateSuggestionsRequest{
		Documents: []Document{{ID: 123, Title: "Invoice", Content: "content"}},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "/api/jobs/suggestions", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	jobs := suggestionJobStore.getAllJobs()
	require.Len(t, jobs, 1)
	assert.Equal(t, "failed", jobs[0].Status)
	assert.Equal(t, "Suggestion queue is full", jobs[0].Error)
}

func TestSuggestionJobStoreReturnsSnapshots(t *testing.T) {
	resetSuggestionJobTestState(t)
	job := &SuggestionJob{
		ID:     "snapshot-job",
		Status: "pending",
		Request: GenerateSuggestionsRequest{
			Documents: []Document{{ID: 1, Tags: []string{"original"}}},
		},
		Result: []DocumentSuggestion{{
			ID:            1,
			SuggestedTags: []string{"suggested"},
		}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	snapshot, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	snapshot.Status = "completed"
	snapshot.Request.Documents[0].Tags[0] = "changed"
	snapshot.Result[0].SuggestedTags[0] = "changed"

	updatedSnapshot, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "pending", updatedSnapshot.Status)
	assert.Equal(t, []string{"original"}, updatedSnapshot.Request.Documents[0].Tags)
	assert.Equal(t, []string{"suggested"}, updatedSnapshot.Result[0].SuggestedTags)

	allSnapshots := suggestionJobStore.getAllJobs()
	require.Len(t, allSnapshots, 1)
	allSnapshots[0].Status = "failed"

	finalSnapshot, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "pending", finalSnapshot.Status)
}

func TestSuggestionJobStoreStartPendingIsAtomic(t *testing.T) {
	resetSuggestionJobTestState(t)
	job := &SuggestionJob{
		ID:        "start-pending",
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	require.True(t, suggestionJobStore.startPending(job.ID))
	assert.False(t, suggestionJobStore.cancelPending(job.ID))
	assert.False(t, suggestionJobStore.startPending(job.ID))

	updatedJob, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "in_progress", updatedJob.Status)
}

func TestProcessSuggestionJobCompletes(t *testing.T) {
	resetSuggestionJobTestState(t)

	app := &App{
		Client: &mockPaperlessClient{},
		LLM:    &mockLLM{},
	}
	job := &SuggestionJob{
		ID: "suggestions-complete",
		Request: GenerateSuggestionsRequest{
			Documents: []Document{{ID: 456, Title: "Invoice", Content: "content", Tags: []string{"paperless-gpt"}}},
		},
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	processSuggestionJob(app, job)

	updatedJob, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "completed", updatedJob.Status)
	assert.Equal(t, 1, updatedJob.DocumentsDone)
	require.Len(t, updatedJob.Result, 1)
	assert.Equal(t, 456, updatedJob.Result[0].ID)
}

func TestStopSuggestionJobHandlerCancelsPendingJob(t *testing.T) {
	resetSuggestionJobTestState(t)
	gin.SetMode(gin.TestMode)

	app := &App{}
	router := gin.Default()
	router.POST("/api/jobs/suggestions/:job_id/stop", app.stopSuggestionJobHandler)

	job := &SuggestionJob{
		ID:        "suggestions-pending",
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	req, err := http.NewRequest(http.MethodPost, "/api/jobs/suggestions/suggestions-pending/stop", nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	updatedJob, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "cancelled", updatedJob.Status)
	assert.Equal(t, "Job cancelled by user", updatedJob.Error)
}

func TestProcessSuggestionJobSkipsAlreadyCancelledJob(t *testing.T) {
	resetSuggestionJobTestState(t)

	app := &App{
		Client: &mockPaperlessClient{},
		LLM:    &mockLLM{},
	}
	job := &SuggestionJob{
		ID: "suggestions-cancelled",
		Request: GenerateSuggestionsRequest{
			Documents:      []Document{{ID: 654, Title: "Invoice", Content: "content"}},
			GenerateTitles: true,
		},
		Status:    "cancelled",
		Error:     "Job cancelled by user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	processSuggestionJob(app, job)

	updatedJob, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "cancelled", updatedJob.Status)
	assert.Empty(t, updatedJob.Result)
}

func TestProcessSuggestionJobTimeout(t *testing.T) {
	resetSuggestionJobTestState(t)
	t.Setenv("SUGGESTION_JOB_TIMEOUT_SECONDS", "1")

	previousTitleTemplate := titleTemplate
	titleTemplate = template.Must(template.New("title").Parse("{{.Content}}"))
	t.Cleanup(func() {
		titleTemplate = previousTitleTemplate
	})

	app := &App{
		Client: &mockPaperlessClient{},
		LLM:    &blockingLLM{},
	}
	job := &SuggestionJob{
		ID: "suggestions-timeout",
		Request: GenerateSuggestionsRequest{
			Documents:      []Document{{ID: 789, Title: "Invoice", Content: "content"}},
			GenerateTitles: true,
		},
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	suggestionJobStore.addJob(job)

	processSuggestionJob(app, job)

	updatedJob, exists := suggestionJobStore.getJob(job.ID)
	require.True(t, exists)
	assert.Equal(t, "failed", updatedJob.Status)
	assert.Equal(t, "Suggestion job timed out", updatedJob.Error)
}
