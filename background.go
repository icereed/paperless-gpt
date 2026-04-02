package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// This is our interface, allowing us to enable proper testing
type BackgroundProcessor interface {
	processAutoOcrTagDocuments(ctx context.Context) (int, error)
	processAutoTagDocuments(ctx context.Context) (int, error)
	isOcrEnabled() bool
}

// Start our background tasks in a goroutine
func StartBackgroundTasks(ctx context.Context, app BackgroundProcessor) {
	go func() {
		minBackoffDuration := 10 * time.Second
		maxBackoffDuration := time.Hour
		pollingInterval := 10 * time.Second

		backoffDuration := minBackoffDuration

		for {
			select {
			case <-ctx.Done():
				log.Infoln("Background tasks shutting down")
				return
			default: // needed to make this non-blocking
			}

			processedCount, err := func() (count int, err error) {
				count = 0

				// If OCR is enabled, run OCR tagging first
				if app.isOcrEnabled() {
					ocrCount, err := app.processAutoOcrTagDocuments(ctx)
					if err != nil {
						return 0, fmt.Errorf("error in processAutoOcrTagDocuments: %w", err)
					}
					count += ocrCount
				}

				// Run auto-tagging after OCR
				autoCount, err := app.processAutoTagDocuments(ctx)
				if err != nil {
					return 0, fmt.Errorf("error in processAutoTagDocuments: %w", err)
				}
				count += autoCount

				return count, nil
			}()

			if err != nil {
				log.Errorf("Error in background tagging: %v", err)
				time.Sleep(backoffDuration)

				// Exponential backoff logic
				backoffDuration *= 2
				if backoffDuration > maxBackoffDuration {
					log.Warnf("Max backoff duration reached. Using %v", maxBackoffDuration)
					backoffDuration = maxBackoffDuration
				}
			} else {
				// Reset backoff when processing succeeds
				backoffDuration = minBackoffDuration
			}

			// If nothing was processed, pause before next cycle
			if processedCount == 0 {
				time.Sleep(pollingInterval)
			}
		}
	}()
}

func enqueueIfNotExists(app *App, docID int, jobType string) (bool, error) {
	var count int64
	err := app.Database.Model(&QueuedJob{}).
		Where("document_id = ? AND job_type = ? AND status IN ?", docID, jobType, []string{"pending", "in_progress", "failed"}).
		Count(&count).Error
	
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil // Already queued
	}

	job := &QueuedJob{
		ID:         generateJobID(),
		DocumentID: docID,
		JobType:    jobType,
		Status:     "pending",
	}
	if err := EnqueueJob(app.Database, job); err != nil {
		return false, err
	}
	log.Infof("Enqueued new %s job for Document %d (Job ID: %s)", jobType, docID, job.ID)
	return true, nil
}

// processAutoTagDocuments handles the background auto-tagging of documents
func (app *App) processAutoTagDocuments(ctx context.Context) (int, error) {
	documents, err := app.Client.GetDocumentsByTag(ctx, autoTag, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoTag: %w", err)
	}

	if len(documents) == 0 {
		return 0, nil // No documents to process
	}

	var errs []error
	processedCount := 0

	for _, docSummary := range documents {
		// Skip documents that have the autoOcrTag
		if slices.Contains(docSummary.Tags, autoOcrTag) {
			continue
		}

		enqueued, err := enqueueIfNotExists(app, docSummary.ID, "auto_tag")
		if err != nil {
			errs = append(errs, err)
		} else if enqueued {
			processedCount++
		}
	}

	if len(errs) > 0 {
		return processedCount, errors.Join(errs...)
	}

	return processedCount, nil
}

// processAutoOcrTagDocuments handles the background auto-tagging of OCR documents
func (app *App) processAutoOcrTagDocuments(ctx context.Context) (int, error) {
	documents, err := app.Client.GetDocumentsByTag(ctx, autoOcrTag, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoOcrTag: %w", err)
	}

	if len(documents) == 0 {
		return 0, nil
	}

	successCount := 0
	var errs []error

	for _, document := range documents {
		// Skip OCR if the document already has the OCR complete tag and tagging is enabled
		if app.pdfOCRTagging {
			hasCompleteTag := false
			for _, tag := range document.Tags {
				if tag == app.pdfOCRCompleteTag {
					hasCompleteTag = true
					break
				}
			}

			if hasCompleteTag {
				// Remove only the autoOcrTag to take it out of the processing queue
				// while preserving the OCR complete tag
				err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{
					{
						ID:               document.ID,
						OriginalDocument: document,
						RemoveTags:       []string{autoOcrTag},
					},
				}, app.Database, false)

				if err != nil {
					errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
					continue
				}

				successCount++
				continue
			}
		}

		enqueued, err := enqueueIfNotExists(app, document.ID, "auto_ocr")
		if err != nil {
			errs = append(errs, err)
		} else if enqueued {
			successCount++
		}
	}

	if len(errs) > 0 {
		return successCount, fmt.Errorf("one or more errors occurred: %w", errors.Join(errs...))
	}

	return successCount, nil
}
