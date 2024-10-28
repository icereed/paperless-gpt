package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Job represents an OCR job
type Job struct {
	ID         string
	DocumentID int
	Status     string // "pending", "in_progress", "completed", "failed"
	Result     string // OCR result or error message
	CreatedAt  time.Time
	UpdatedAt  time.Time
	PagesDone  int // Number of pages processed
}

// JobStore manages jobs and their statuses
type JobStore struct {
	sync.RWMutex
	jobs map[string]*Job
}

var (
	jobStore = &JobStore{
		jobs: make(map[string]*Job),
	}
	jobQueue = make(chan *Job, 100) // Buffered channel with capacity of 100 jobs
	logger   = log.New(os.Stdout, "OCR_JOB: ", log.LstdFlags)
)

func generateJobID() string {
	return uuid.New().String()
}

func (store *JobStore) addJob(job *Job) {
	store.Lock()
	defer store.Unlock()
	job.PagesDone = 0 // Initialize PagesDone to 0
	store.jobs[job.ID] = job
	logger.Printf("Job added: %v", job)
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
		logger.Printf("Job status updated: %v", job)
	}
}

func (store *JobStore) updatePagesDone(jobID string, pagesDone int) {
	store.Lock()
	defer store.Unlock()
	if job, exists := store.jobs[jobID]; exists {
		job.PagesDone = pagesDone
		job.UpdatedAt = time.Now()
		logger.Printf("Job pages done updated: %v", job)
	}
}

func startWorkerPool(app *App, numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			logger.Printf("Worker %d started", workerID)
			for job := range jobQueue {
				logger.Printf("Worker %d processing job: %s", workerID, job.ID)
				processJob(app, job)
			}
		}(i)
	}
}

func processJob(app *App, job *Job) {
	jobStore.updateJobStatus(job.ID, "in_progress", "")

	ctx := context.Background()

	// Download images of the document
	imagePaths, err := app.Client.DownloadDocumentAsImages(ctx, job.DocumentID)
	if err != nil {
		logger.Printf("Error downloading document images for job %s: %v", job.ID, err)
		jobStore.updateJobStatus(job.ID, "failed", fmt.Sprintf("Error downloading document images: %v", err))
		return
	}

	var ocrTexts []string
	for i, imagePath := range imagePaths {
		imageContent, err := os.ReadFile(imagePath)
		if err != nil {
			logger.Printf("Error reading image file for job %s: %v", job.ID, err)
			jobStore.updateJobStatus(job.ID, "failed", fmt.Sprintf("Error reading image file: %v", err))
			return
		}

		ocrText, err := app.doOCRViaLLM(ctx, imageContent)
		if err != nil {
			logger.Printf("Error performing OCR for job %s: %v", job.ID, err)
			jobStore.updateJobStatus(job.ID, "failed", fmt.Sprintf("Error performing OCR: %v", err))
			return
		}

		ocrTexts = append(ocrTexts, ocrText)
		jobStore.updatePagesDone(job.ID, i+1) // Update PagesDone after each page is processed
	}

	// Combine the OCR texts
	fullOcrText := strings.Join(ocrTexts, "\n\n")

	// Update job status and result
	jobStore.updateJobStatus(job.ID, "completed", fullOcrText)
	logger.Printf("Job completed: %s", job.ID)
}
