package main

import (
	"context"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SuggestionJob represents an asynchronous metadata suggestion generation job.
type SuggestionJob struct {
	ID                string
	Status            string // "pending", "in_progress", "completed", "failed", "cancelled"
	Request           GenerateSuggestionsRequest
	Result            []DocumentSuggestion
	FailedDocuments   []SuggestionJobDocumentFailure
	Error             string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DocumentsDone     int
	TotalDocuments    int
	CurrentDocumentID int
}

// SuggestionJobDocumentFailure records a single document that failed inside a job
// while the rest of the batch kept going.
type SuggestionJobDocumentFailure struct {
	DocumentID    int    `json:"document_id"`
	DocumentTitle string `json:"document_title"`
	Error         string `json:"error"`
}

// SuggestionJobStore manages suggestion jobs and their statuses.
type SuggestionJobStore struct {
	sync.RWMutex
	jobs map[string]*SuggestionJob
}

var (
	suggestionJobCancellersMu sync.Mutex
	suggestionJobCancellers   = make(map[string]context.CancelFunc)

	suggestionJobStore = &SuggestionJobStore{
		jobs: make(map[string]*SuggestionJob),
	}
	suggestionJobQueue = make(chan *SuggestionJob, 100)
	suggestionLogger   = logrus.New()
)

func init() {
	suggestionLogger.SetOutput(os.Stdout)
	suggestionLogger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	suggestionLogger.SetLevel(logrus.InfoLevel)
}

func (store *SuggestionJobStore) addJob(job *SuggestionJob) {
	store.Lock()
	defer store.Unlock()
	job.DocumentsDone = 0
	job.TotalDocuments = len(job.Request.Documents)
	store.jobs[job.ID] = job
	suggestionLogger.Infof("Suggestion job added: %s", job.ID)
}

func cloneSuggestionJob(job *SuggestionJob) SuggestionJob {
	jobCopy := *job
	if job.Request.Documents != nil {
		jobCopy.Request.Documents = cloneDocuments(job.Request.Documents)
	}
	if job.Result != nil {
		jobCopy.Result = cloneDocumentSuggestions(job.Result)
	}
	if job.FailedDocuments != nil {
		jobCopy.FailedDocuments = append([]SuggestionJobDocumentFailure(nil), job.FailedDocuments...)
	}
	return jobCopy
}

func cloneDocuments(documents []Document) []Document {
	documentCopies := make([]Document, len(documents))
	for i, doc := range documents {
		documentCopies[i] = doc
		if doc.Tags != nil {
			documentCopies[i].Tags = append([]string(nil), doc.Tags...)
		}
		if doc.CustomFields != nil {
			documentCopies[i].CustomFields = append([]CustomFieldResponse(nil), doc.CustomFields...)
		}
	}
	return documentCopies
}

func cloneDocumentSuggestions(suggestions []DocumentSuggestion) []DocumentSuggestion {
	suggestionCopies := make([]DocumentSuggestion, len(suggestions))
	for i, suggestion := range suggestions {
		suggestionCopies[i] = suggestion
		suggestionCopies[i].OriginalDocument = cloneDocuments([]Document{suggestion.OriginalDocument})[0]
		if suggestion.SuggestedTags != nil {
			suggestionCopies[i].SuggestedTags = append([]string(nil), suggestion.SuggestedTags...)
		}
		if suggestion.SuggestedCustomFields != nil {
			suggestionCopies[i].SuggestedCustomFields = append([]CustomFieldSuggestion(nil), suggestion.SuggestedCustomFields...)
		}
		if suggestion.RemoveTags != nil {
			suggestionCopies[i].RemoveTags = append([]string(nil), suggestion.RemoveTags...)
		}
		if suggestion.AddTags != nil {
			suggestionCopies[i].AddTags = append([]string(nil), suggestion.AddTags...)
		}
	}
	return suggestionCopies
}

func (store *SuggestionJobStore) getJob(jobID string) (SuggestionJob, bool) {
	store.RLock()
	defer store.RUnlock()
	job, exists := store.jobs[jobID]
	if !exists {
		return SuggestionJob{}, false
	}
	return cloneSuggestionJob(job), true
}

func (store *SuggestionJobStore) getAllJobs() []SuggestionJob {
	store.RLock()
	defer store.RUnlock()

	jobs := make([]SuggestionJob, 0, len(store.jobs))
	for _, job := range store.jobs {
		jobs = append(jobs, cloneSuggestionJob(job))
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	return jobs
}

func (store *SuggestionJobStore) startPending(jobID string) bool {
	store.Lock()
	defer store.Unlock()

	job, exists := store.jobs[jobID]
	if !exists || job.Status != "pending" {
		return false
	}

	job.Status = "in_progress"
	job.Error = ""
	job.UpdatedAt = time.Now()
	return true
}

func (store *SuggestionJobStore) updateStatus(jobID, status, message string) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.Status = status
		if message != "" {
			job.Error = message
		}
		job.UpdatedAt = time.Now()
	}
}

func (store *SuggestionJobStore) cancelPending(jobID string) bool {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists && job.Status == "pending" {
		job.Status = "cancelled"
		job.Error = "Job cancelled by user"
		job.UpdatedAt = time.Now()
		return true
	}
	return false
}

func (store *SuggestionJobStore) isCancelled(jobID string) bool {
	store.RLock()
	defer store.RUnlock()
	job, exists := store.jobs[jobID]
	return exists && job.Status == "cancelled"
}

func (store *SuggestionJobStore) updateProgress(jobID string, documentsDone int, currentDocumentID int) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.DocumentsDone = documentsDone
		job.CurrentDocumentID = currentDocumentID
		job.UpdatedAt = time.Now()
	}
}

func (store *SuggestionJobStore) complete(jobID string, result []DocumentSuggestion, failures []SuggestionJobDocumentFailure) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.Status = "completed"
		job.Result = result
		job.FailedDocuments = failures
		job.Error = ""
		job.DocumentsDone = job.TotalDocuments
		job.CurrentDocumentID = 0
		job.UpdatedAt = time.Now()
	}
}

func (store *SuggestionJobStore) failWithFailures(jobID, message string, failures []SuggestionJobDocumentFailure) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.Status = "failed"
		job.Error = message
		job.FailedDocuments = failures
		job.UpdatedAt = time.Now()
	}
}

func startSuggestionWorkerPool(app *App, numWorkers int) {
	if numWorkers < 1 {
		numWorkers = 1
	}

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			suggestionLogger.Infof("Suggestion worker %d started", workerID)
			for job := range suggestionJobQueue {
				suggestionLogger.Infof("Suggestion worker %d processing job: %s", workerID, job.ID)
				processSuggestionJob(app, job)
			}
		}(i)
	}
}

func processSuggestionJob(app *App, job *SuggestionJob) {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	jobCtx := baseCtx
	cancel := baseCancel
	if timeout := suggestionJobTimeout(); timeout > 0 {
		var timeoutCancel context.CancelFunc
		jobCtx, timeoutCancel = context.WithTimeout(baseCtx, timeout)
		cancel = func() {
			timeoutCancel()
			baseCancel()
		}
	}

	suggestionJobCancellersMu.Lock()
	suggestionJobCancellers[job.ID] = cancel
	suggestionJobCancellersMu.Unlock()
	defer func() {
		cancel()
		suggestionJobCancellersMu.Lock()
		delete(suggestionJobCancellers, job.ID)
		suggestionJobCancellersMu.Unlock()
	}()

	if !suggestionJobStore.startPending(job.ID) {
		suggestionLogger.Infof("Suggestion job skipped because it was not pending: %s", job.ID)
		return
	}

	result, failures, err := app.generateDocumentSuggestionsForJob(jobCtx, job.Request, job.ID, suggestionLogger.WithField("job_id", job.ID))
	if err != nil {
		switch jobCtx.Err() {
		case context.Canceled:
			suggestionJobStore.updateStatus(job.ID, "cancelled", "Job cancelled by user")
			suggestionLogger.Infof("Suggestion job cancelled: %s", job.ID)
		case context.DeadlineExceeded:
			suggestionJobStore.updateStatus(job.ID, "failed", "Suggestion job timed out")
			suggestionLogger.Warnf("Suggestion job timed out: %s", job.ID)
		default:
			suggestionJobStore.updateStatus(job.ID, "failed", err.Error())
			suggestionLogger.Errorf("Suggestion job failed: %s: %v", job.ID, err)
		}
		return
	}

	if len(result) == 0 && len(failures) > 0 {
		suggestionJobStore.failWithFailures(job.ID, "All documents failed", failures)
		suggestionLogger.Warnf("Suggestion job failed for all %d documents: %s", len(failures), job.ID)
		return
	}

	suggestionJobStore.complete(job.ID, result, failures)
	suggestionLogger.Infof("Suggestion job completed: %s (%d ok, %d failed)", job.ID, len(result), len(failures))
}

func suggestionJobTimeout() time.Duration {
	value := os.Getenv("SUGGESTION_JOB_TIMEOUT_SECONDS")
	if value == "" {
		return 0
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}

	return time.Duration(seconds) * time.Second
}

func suggestionWorkerCount() int {
	value := os.Getenv("SUGGESTION_WORKERS")
	if value == "" {
		return 1
	}

	workers, err := strconv.Atoi(value)
	if err != nil || workers < 1 {
		return 1
	}

	return workers
}
