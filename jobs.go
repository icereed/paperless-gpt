package main

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var (
	jobCancellersMu sync.Mutex
	jobCancellers   = make(map[string]context.CancelFunc)

	reOcrCancellersMu sync.Mutex
	reOcrCancellers   = make(map[string]context.CancelFunc)

	logger = logrus.New()
)

func init() {
	logger.SetOutput(os.Stdout)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logger.SetLevel(logrus.InfoLevel)
	logger.WithField("prefix", "JOB_QUEUE")
}

func generateJobID() string {
	return uuid.New().String()
}

// EnqueueJob inserts a new job into the database queue.
func EnqueueJob(db *gorm.DB, job *QueuedJob) error {
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	if job.MaxRetries == 0 {
		job.MaxRetries = 3 // default 3 retries
	}
	return db.Create(job).Error
}

// GetJob retrieves a queued job by ID.
func GetJob(db *gorm.DB, jobID string) (*QueuedJob, error) {
	var job QueuedJob
	err := db.First(&job, "id = ?", jobID).Error
	return &job, err
}

// GetAllJobs retrieves all queued jobs safely.
func GetAllJobs(db *gorm.DB) ([]QueuedJob, error) {
	var jobs []QueuedJob
	err := db.Order("created_at DESC").Find(&jobs).Error
	return jobs, err
}

func UpdateJobStatus(db *gorm.DB, jobID, status, result string) error {
	return db.Model(&QueuedJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"status":     status,
		"result":     result,
		"updated_at": time.Now(),
	}).Error
}

func UpdatePagesDone(db *gorm.DB, jobID string, pagesDone int) error {
	return db.Model(&QueuedJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"pages_done": pagesDone,
		"updated_at": time.Now(),
	}).Error
}

func handleJobFailure(db *gorm.DB, job *QueuedJob, err error) {
	job.Attempts++
	if job.Attempts >= job.MaxRetries {
		job.Status = "permanently_failed"
		job.Result = err.Error()
	} else {
		job.Status = "failed"
		job.Result = err.Error()
		// Exponential backoff: 30s, 1m, 2m
		backoffDuration := time.Duration(1<<job.Attempts) * 30 * time.Second
		job.NextRetryAt = time.Now().Add(backoffDuration)
	}
	job.UpdatedAt = time.Now()
	db.Save(job)
}

func startWorkerPool(app *App, numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			logger.Infof("Worker %d started", workerID)
			for {
				var job QueuedJob
				// Lock a job prioritizing older pending jobs or older failed jobs ready for retry
				tx := app.Database.Raw(`
					UPDATE queued_jobs 
					SET status = 'in_progress', updated_at = ?
					WHERE id = (
						SELECT id FROM queued_jobs 
						WHERE status = 'pending' OR (status = 'failed' AND attempts < max_retries AND next_retry_at <= ?)
						ORDER BY created_at ASC 
						LIMIT 1
					)
					RETURNING *;
				`, time.Now(), time.Now()).Scan(&job)

				if tx.Error != nil || tx.RowsAffected == 0 {
					// No jobs found, sleep
					time.Sleep(2 * time.Second)
					continue
				}

				logger.Infof("Worker %d processing job: %s, Type: %s, Attempt: %d", workerID, job.ID, job.JobType, job.Attempts+1)
				processJob(app, &job)
			}
		}(i)
	}
}

func processJob(app *App, job *QueuedJob) {
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

	var err error
	switch job.JobType {
	case "manual_ocr", "auto_ocr":
		err = processOCRJob(app, jobCtx, job)
	case "auto_tag":
		err = processAutoTagJob(app, jobCtx, job)
	default:
		logger.Warnf("Unknown job type: %s", job.JobType)
		UpdateJobStatus(app.Database, job.ID, "permanently_failed", "Unknown job type")
		return
	}

	if err != nil {
		if jobCtx.Err() == context.Canceled {
			UpdateJobStatus(app.Database, job.ID, "cancelled", "Job cancelled by user")
			logger.Infof("Job cancelled: %s", job.ID)
		} else {
			logger.Errorf("Error processing job %s: %v", job.ID, err)
			handleJobFailure(app.Database, job, err)
		}
		return
	}

	UpdateJobStatus(app.Database, job.ID, "completed", job.Result)
	logger.Infof("Job completed: %s", job.ID)
}

// processOCRJob handles both manual and automatic OCR tasks
func processOCRJob(app *App, ctx context.Context, job *QueuedJob) error {
	// Delete old OCR page results for this document before starting new OCR
	if err := DeleteOcrPageResults(app.Database, job.DocumentID); err != nil {
		logger.Errorf("Failed to delete old OCR page results for document %d: %v", job.DocumentID, err)
	}

	options := OCROptions{
		UploadPDF:       app.pdfUpload,
		ReplaceOriginal: app.pdfReplace,
		CopyMetadata:    app.pdfCopyMetadata,
		LimitPages:      limitOcrPages,
		ProcessMode:     app.ocrProcessMode,
	}
	if job.OptionsJSON != "" {
		_ = json.Unmarshal([]byte(job.OptionsJSON), &options)
	}

	jobIDForLog := ""
	if job.JobType == "manual_ocr" {
		jobIDForLog = job.ID
	}

	// Process document OCR via the injected processor or native
	var processedDoc *ProcessedDocument
	var err error
	if app.docProcessor != nil {
		processedDoc, err = app.docProcessor.ProcessDocumentOCR(ctx, job.DocumentID, options, jobIDForLog)
	} else {
		processedDoc, err = app.ProcessDocumentOCR(ctx, job.DocumentID, options, jobIDForLog)
	}

	if err != nil {
		return err
	}
	if processedDoc == nil {
		job.Result = "Skipped (already processed or other reason)"
		return nil
	}

	job.Result = processedDoc.Text

	// If this is an auto_ocr job, we need to update tags in Paperless
	if job.JobType == "auto_ocr" {
		doc, err := app.Client.GetDocument(ctx, job.DocumentID)
		if err == nil {
			documentSuggestion := DocumentSuggestion{
				ID:               job.DocumentID,
				OriginalDocument: doc,
				SuggestedContent: processedDoc.Text,
				RemoveTags:       []string{autoOcrTag},
				AddTags: func() []string {
					if app.pdfOCRTagging && !options.UploadPDF {
						return []string{app.pdfOCRCompleteTag}
					}
					return nil
				}(),
			}

			if (app.pdfOCRTagging) && app.pdfOCRCompleteTag != "" {
				documentSuggestion.SuggestedTags = []string{app.pdfOCRCompleteTag}
				documentSuggestion.KeepOriginalTags = true
			}

			if !options.ReplaceOriginal || !processedDoc.ReplacedOriginal {
				if err := app.Client.UpdateDocuments(ctx, []DocumentSuggestion{documentSuggestion}, app.Database, false); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// processAutoTagJob processes the auto-tagging of a document
func processAutoTagJob(app *App, ctx context.Context, job *QueuedJob) error {
	document, err := app.Client.GetDocument(ctx, job.DocumentID)
	if err != nil {
		return err
	}

	docLogger := documentLogger(job.DocumentID)
	suggestionRequest := GenerateSuggestionsRequest{
		Documents:              []Document{document},
		GenerateTitles:         true,  // Ideally we pull this from settings dynamically, handled internally
		GenerateTags:           true,
		GenerateCorrespondents: true,
		GenerateDocumentTypes:  true,
		GenerateCreatedDate:    true,
		GenerateCustomFields:   true,  // Validated internally based on settings
	}

	suggestions, err := app.generateDocumentSuggestions(ctx, suggestionRequest, docLogger)
	if err != nil {
		return err
	}

	if err := app.Client.UpdateDocuments(ctx, suggestions, app.Database, false); err != nil {
		return err
	}

	return nil
}
