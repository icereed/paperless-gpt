package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var autoTagDeprecationWarnOnce sync.Once

// This is our interface, allowing us to enable proper testing
type BackgroundProcessor interface {
	processAutoOcrTagDocuments(ctx context.Context) (int, error)
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

				if autoTag != "" {
					autoTagDeprecationWarnOnce.Do(func() {
						log.Warn("AUTO_TAG metadata auto-apply was removed in 0.7.0.")
					})
				}

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

// processAutoOcrTagDocuments handles the background auto-tagging of OCR documents
func (app *App) processAutoOcrTagDocuments(ctx context.Context) (int, error) {
	documents, err := app.Client.GetDocumentsByTag(ctx, autoOcrTag, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoOcrTag: %w", err)
	}

	if len(documents) == 0 {
		log.Debugf("No documents with tag %s found", autoOcrTag)
		return 0, nil
	}

	log.Debugf("Found %d documents with tag %s", len(documents), autoOcrTag)

	successCount := 0
	var errs []error

	for _, document := range documents {
		docLogger := documentLogger(document.ID)
		docLogger.Info("Processing document for OCR")

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
				docLogger.Infof("Document already has OCR complete tag '%s', skipping OCR processing", app.pdfOCRCompleteTag)

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
					docLogger.Errorf("Update to remove autoOcrTag failed: %v", err)
					errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
					continue
				}

				docLogger.Info("Successfully removed auto OCR tag")
				successCount++
				continue
			}
		}

		options := OCROptions{
			UploadPDF:       app.pdfUpload,
			ReplaceOriginal: app.pdfReplace,
			CopyMetadata:    app.pdfCopyMetadata,
			LimitPages:      limitOcrPages,
			ProcessMode:     app.ocrProcessMode,
		}

		// Use the DocumentProcessor interface instead of calling the method directly
		var processedDoc *ProcessedDocument
		var err error
		if app.docProcessor != nil {
			// Use injected processor if available
			processedDoc, err = app.docProcessor.ProcessDocumentOCR(ctx, document.ID, options, "")
		} else {
			// Use the app's own implementation if no processor is injected
			processedDoc, err = app.ProcessDocumentOCR(ctx, document.ID, options, "")
		}

		if err != nil {
			docLogger.Errorf("OCR processing failed: %v", err)
			errs = append(errs, fmt.Errorf("document %d OCR error: %w", document.ID, err))
			continue
		}
		if processedDoc == nil {
			docLogger.Info("OCR processing skipped for document")
			continue
		}
		docLogger.Debug("OCR processing completed")

		documentSuggestion := DocumentSuggestion{
			ID:               document.ID,
			OriginalDocument: document,
			SuggestedContent: processedDoc.Text,
			RemoveTags:       []string{autoOcrTag},
		}

		if app.pdfOCRTagging && app.pdfOCRCompleteTag != "" {
			// Merge the OCR complete tag into the existing tags
			documentSuggestion.SuggestedTags = []string{app.pdfOCRCompleteTag}
			documentSuggestion.KeepOriginalTags = true
			docLogger.Infof("Adding OCR complete tag '%s'", app.pdfOCRCompleteTag)
		}

		// Skip updating the original document if it was actually replaced (deleted) during OCR.
		// The replacement document will be processed as a new document on the next cycle.
		if options.ReplaceOriginal && processedDoc != nil && processedDoc.ReplacedOriginal {
			docLogger.Info("Skipping tag update for replaced document (original was deleted)")
		} else {
			err = app.Client.UpdateDocuments(ctx, []DocumentSuggestion{
				documentSuggestion,
			}, app.Database, false)
			if err != nil {
				docLogger.Errorf("Update after OCR failed: %v", err)
				errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
				continue
			}
		}

		docLogger.Info("Successfully processed document OCR")
		successCount++
	}

	if len(errs) > 0 {
		return successCount, fmt.Errorf("one or more errors occurred: %w", errors.Join(errs...))
	}

	return successCount, nil
}
