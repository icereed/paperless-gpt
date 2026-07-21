package main

import (
	"context"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var (
	jobCancellersMu sync.Mutex
	jobCancellers   = make(map[string]context.CancelFunc)

	reOcrCancellersMu sync.Mutex
	reOcrCancellers   = make(map[string]context.CancelFunc)
)

// Job represents an OCR job
type Job struct {
	ID         string
	DocumentID int
	Status     string // "pending", "in_progress", "completed", "failed", "cancelled"
	Result     string // OCR result (combined text) or error message
	CreatedAt  time.Time
	UpdatedAt  time.Time
	PagesDone  int        // Number of pages processed
	TotalPages int        // Total number of pages in the document
	Options    OCROptions // OCR processing options
}

// JobStore manages jobs and their statuses
type JobStore struct {
	sync.RWMutex
	jobs map[string]*Job
}

var (
	logger = logrus.New()

	jobStore = &JobStore{
		jobs: make(map[string]*Job),
	}
	jobQueue = make(chan *Job, 100) // Buffered channel with capacity of 100 jobs
)

func init() {

	// Initialize logger
	logger.SetOutput(os.Stdout)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logger.SetLevel(logrus.InfoLevel)
	logger.WithField("prefix", "OCR_JOB")
}

func generateJobID() string {
	return uuid.New().String()
}

// maxOCRJobs caps the in-memory OCR job store; terminal jobs are evicted
// oldest-first so a long-running server does not accumulate them without bound.
// (The durable record lives in the OCRRun table, which has its own pruning.)
const maxOCRJobs = 200

func (store *JobStore) addJob(job *Job) {
	store.Lock()
	defer store.Unlock()
	job.PagesDone = 0 // Initialize PagesDone to 0
	store.jobs[job.ID] = job
	store.evictOldestTerminalLocked()
	logger.Infof("Job added: %v", job)
}

// evictOldestTerminalLocked drops the oldest finished jobs while over capacity.
// Callers must hold the write lock; in-flight jobs are never evicted.
func (store *JobStore) evictOldestTerminalLocked() {
	if len(store.jobs) <= maxOCRJobs {
		return
	}
	type terminal struct {
		id      string
		updated time.Time
	}
	var candidates []terminal
	for id, j := range store.jobs {
		switch j.Status {
		case "completed", "failed", "cancelled":
			candidates = append(candidates, terminal{id, j.UpdatedAt})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].updated.Before(candidates[j].updated)
	})
	for _, c := range candidates {
		if len(store.jobs) <= maxOCRJobs {
			break
		}
		delete(store.jobs, c.id)
	}
}

func (store *JobStore) getJob(jobID string) (*Job, bool) {
	store.RLock()
	defer store.RUnlock()
	job, exists := store.jobs[jobID]
	return job, exists
}

func (store *JobStore) GetAllJobs() []*Job {
	store.RLock()
	defer store.RUnlock()

	jobs := make([]*Job, 0, len(store.jobs))
	for _, job := range store.jobs {
		jobs = append(jobs, job)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	return jobs
}

func (store *JobStore) updateJobStatus(jobID, status, result string) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.Status = status
		if result != "" {
			job.Result = result
		}
		job.UpdatedAt = time.Now()
		logger.Infof("Job status updated: %v", job)
	}
}

func (store *JobStore) updatePagesDone(jobID string, pagesDone int) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.PagesDone = pagesDone
		job.UpdatedAt = time.Now()
		logger.Infof("Job pages done updated: %v", job)
	}
}

// progress returns the current page counters of a job.
func (store *JobStore) progress(jobID string) (pagesDone, totalPages int) {
	store.RLock()
	defer store.RUnlock()
	if job, exists := store.jobs[jobID]; exists {
		return job.PagesDone, job.TotalPages
	}
	return 0, 0
}

func startWorkerPool(app *App, numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			logger.Infof("Worker %d started", workerID)
			for job := range jobQueue {
				logger.Infof("Worker %d processing job: %s", workerID, job.ID)
				processJob(app, job)
			}
		}(i)
	}
}

func processJob(app *App, job *Job) {
	jobStore.updateJobStatus(job.ID, "in_progress", "")

	jobCtx, cancel := context.WithCancel(context.Background())
	jobCancellersMu.Lock()
	jobCancellers[job.ID] = cancel
	jobCancellersMu.Unlock()
	defer func() {
		cancel()
		jobCancellersMu.Lock()
		delete(jobCancellers, job.ID)
		jobCancellersMu.Unlock()
	}()

	// Create OCR options from job options or the effective defaults
	// (settings-persisted values override env-derived ones).
	options := job.Options
	if (options == OCROptions{}) {
		options = app.effectiveOCRDefaults()
	}

	processedDoc, err := app.ProcessDocumentOCR(jobCtx, job.DocumentID, options, job.ID)
	pagesDone, totalPages := jobStore.progress(job.ID)
	if err != nil {
		if jobCtx.Err() == context.Canceled {
			jobStore.updateJobStatus(job.ID, "cancelled", "Job cancelled by user")
			finishOCRRunLogged(app, job.ID, "cancelled", "Job cancelled by user", pagesDone, totalPages, "", "")
			logger.Infof("Job cancelled: %s", job.ID)
		} else {
			logger.Errorf("Error processing document OCR for job %s: %v", job.ID, err)
			jobStore.updateJobStatus(job.ID, "failed", err.Error())
			finishOCRRunLogged(app, job.ID, "failed", err.Error(), pagesDone, totalPages, "", "")
		}
		return
	}
	if processedDoc == nil {
		logger.Infof("OCR processing skipped for job %s (document %d)", job.ID, job.DocumentID)
		jobStore.updateJobStatus(job.ID, "completed", "Skipped (already processed or other reason)")
		finishOCRRunLogged(app, job.ID, "completed", "", pagesDone, totalPages, "none", "Skipped (already processed)")
		return
	}

	jobStore.updateJobStatus(job.ID, "completed", processedDoc.Text)
	finishOCRRunLogged(app, job.ID, "completed", "", pagesDone, totalPages, processedDoc.PDFAction, processedDoc.PDFDetail)
	if err := PruneOCRRuns(app.Database, job.DocumentID); err != nil {
		logger.Warnf("Failed to prune OCR runs for document %d: %v", job.DocumentID, err)
	}
	logger.Infof("Job completed: %s", job.ID)
}

// finishOCRRunLogged persists the run outcome; persistence problems are
// logged, never fatal for the job itself.
func finishOCRRunLogged(app *App, jobID, status, errorMsg string, pagesDone, totalPages int, pdfAction, pdfDetail string) {
	if err := FinishOCRRun(app.Database, jobID, status, errorMsg, pagesDone, totalPages, pdfAction, pdfDetail); err != nil {
		logger.Warnf("Failed to persist OCR run outcome for job %s: %v", jobID, err)
	}
}
