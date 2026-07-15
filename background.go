package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
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
				// Only run auto-tagging if OCR did not find any documents to process, otherwise re-run OCR
				if count == 0 {
					autoCount, err := app.processAutoTagDocuments(ctx)
					if err != nil {
						return 0, fmt.Errorf("error in processAutoTagDocuments: %w", err)
					}
					count += autoCount
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

// applyFailTagAfterPartialSuccess applies the fail tag to a document whose
// update succeeded only after paperless-gpt had to drop one or more fields
// rejected by paperless-ngx (see UpdateDocuments' strip-and-retry path).
//
// The document's tags in paperless-ngx have already been updated by the
// successful retry to whatever the LLM suggested (the auto tag is no longer
// present). To avoid clobbering those LLM-suggested tags, this function
// re-fetches the document's current state, then PATCHes only the tags field
// to append the fail tag.
//
// This is best-effort: if the re-fetch or the PATCH fails, the dropped-field
// information is logged but the document is left with no fail tag. The loop
// is still broken (the successful retry removed the auto tag) — only the
// user-visible marker is missing.
func applyFailTagAfterPartialSuccess(ctx context.Context, client ClientInterface, db *gorm.DB, documentID int, droppedFields []string) {
	docLogger := documentLogger(documentID)
	if failTag == "" {
		docLogger.Warnf("Document %d update succeeded after paperless-ngx rejected fields %v; no FAIL_TAG is configured, so the document is not marked for review.", documentID, droppedFields)
		return
	}
	currentDoc, err := client.GetDocument(ctx, documentID)
	if err != nil {
		docLogger.Errorf("Document %d update succeeded after dropping fields %v, but fetching current state to apply fail tag failed: %v", documentID, droppedFields, err)
		return
	}
	if slices.Contains(currentDoc.Tags, failTag) {
		docLogger.Warnf("Document %d update succeeded after dropping fields %v; fail tag %q is already present.", documentID, droppedFields, failTag)
		return
	}
	suggestion := DocumentSuggestion{
		ID:               documentID,
		OriginalDocument: currentDoc,
		SuggestedTags:    []string{failTag},
		KeepOriginalTags: true,
	}
	if err := client.UpdateDocuments(ctx, []DocumentSuggestion{suggestion}, db, false); err != nil {
		docLogger.Errorf("Document %d update succeeded after dropping fields %v, but applying fail tag %q failed: %v", documentID, droppedFields, failTag, err)
		return
	}
	docLogger.Warnf("Document %d update succeeded after paperless-ngx rejected fields %v; fail tag %q applied for user review.", documentID, droppedFields, failTag)
}

// recoverFromFailedUpdate is called when an UpdateDocuments call has failed for
// a document picked up by the auto-tagging or auto-OCR poll. It performs a
// minimal tag-only PATCH that removes the auto-tag the document was picked up by
// (so the document is not re-processed on every poll cycle, which can cost
// real money on paid LLM providers) and, if failTag is configured, adds it as
// a marker so the user can find and review failed documents.
//
// The recovery PATCH only manipulates tags and therefore should succeed even
// when the original PATCH was rejected for a field-validation reason (e.g.
// an LLM-suggested date that is not a real calendar date).
//
// On its own failure, this function logs at error level but does not return
// the error to the caller — the caller has already recorded the original
// update failure and the recovery is best-effort.
func recoverFromFailedUpdate(ctx context.Context, client ClientInterface, db *gorm.DB, document Document, removeTag string) {
	docLogger := documentLogger(document.ID)
	recoveryFields := DocumentSuggestion{
		ID:               document.ID,
		OriginalDocument: document,
		RemoveTags:       []string{removeTag},
	}
	if failTag != "" {
		recoveryFields.SuggestedTags = []string{failTag}
		recoveryFields.KeepOriginalTags = true
	}
	if err := client.UpdateDocuments(ctx, []DocumentSuggestion{recoveryFields}, db, false); err != nil {
		docLogger.Errorf("Recovery update for failed document %d also failed: %v. The %q tag may still be present and the document may be re-processed on the next poll cycle.", document.ID, err, removeTag)
		return
	}
	if failTag != "" {
		docLogger.Warnf("Document %d update failed; %q tag removed and %q tag applied to break the processing loop.", document.ID, removeTag, failTag)
	} else {
		docLogger.Warnf("Document %d update failed; %q tag removed to break the processing loop (no failTag configured).", document.ID, removeTag)
	}
}

// processAutoTagDocuments handles the background auto-tagging of documents
func (app *App) processAutoTagDocuments(ctx context.Context) (int, error) {
	documents, err := app.Client.GetDocumentsByTag(ctx, autoTag, 25)
	if err != nil {
		return 0, fmt.Errorf("error fetching documents with autoTag: %w", err)
	}

	if len(documents) == 0 {
		log.Debugf("No documents with tag %s found", autoTag)
		return 0, nil // No documents to process
	}

	// Refresh the custom fields cache before processing, as we have documents
	refreshCustomFieldsCache(app.Client)

	log.Debugf("Found at least %d remaining documents with tag %s", len(documents), autoTag)

	var errs []error
	processedCount := 0

	for _, docSummary := range documents {
		// Get the full document details, including custom fields
		document, err := app.Client.GetDocument(ctx, docSummary.ID)
		if err != nil {
			err = fmt.Errorf("error fetching full details for document %d: %w", docSummary.ID, err)
			documentLogger(docSummary.ID).Error(err.Error())
			errs = append(errs, err)
			continue
		}

		// Skip documents that have the autoOcrTag
		if slices.Contains(document.Tags, autoOcrTag) {
			log.Debugf("Skipping document %d as it has the OCR tag %s", document.ID, autoOcrTag)
			continue
		}

		docLogger := documentLogger(document.ID)
		docLogger.Info("Processing document for auto-tagging")

		settingsMutex.RLock()
		generateCustomFields := settings.CustomFieldsEnable
		settingsMutex.RUnlock()

		suggestionRequest := GenerateSuggestionsRequest{
			Documents:              []Document{document},
			GenerateTitles:         strings.ToLower(autoGenerateTitle) != "false",
			GenerateTags:           strings.ToLower(autoGenerateTags) != "false",
			GenerateCorrespondents: strings.ToLower(autoGenerateCorrespondents) != "false",
			GenerateDocumentTypes:  strings.ToLower(autoGenerateDocumentType) != "false",
			GenerateCreatedDate:    strings.ToLower(autoGenerateCreatedDate) != "false",
			GenerateCustomFields:   generateCustomFields,
			IsAutoProcessing:       true,
		}

		suggestions, err := app.generateDocumentSuggestions(ctx, suggestionRequest, docLogger)
		if err != nil {
			err = fmt.Errorf("error generating suggestions for document %d: %w", document.ID, err)
			docLogger.Error(err.Error())
			errs = append(errs, err)
			continue
		}

		err = app.Client.UpdateDocuments(ctx, suggestions, app.Database, false)
		if err != nil {
			var partial *PartialUpdateError
			if errors.As(err, &partial) {
				// Update went through but paperless-ngx rejected some fields,
				// which UpdateDocuments dropped in order to land the rest.
				// The auto tag is already gone (it was part of the successful
				// retry's tag update). Apply the fail tag so the user sees
				// that this document needs review.
				applyFailTagAfterPartialSuccess(ctx, app.Client, app.Database, partial.DocumentID, partial.DroppedFields)
				processedCount++
				continue
			}
			err = fmt.Errorf("error updating document %d: %w", document.ID, err)
			docLogger.Error(err.Error())
			errs = append(errs, err)
			recoverFromFailedUpdate(ctx, app.Client, app.Database, document, autoTag)
			continue
		}

		docLogger.Info("Successfully processed document")
		processedCount++
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
			ExistingContent: document.Content,
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
			// Add OCR complete tag if tagging is enabled and PDF wasn't uploaded (upload handles tagging)
			AddTags: func() []string {
				if app.pdfOCRTagging && !options.UploadPDF {
					return []string{app.pdfOCRCompleteTag}
				}
				return nil
			}(),
		}

		if (app.pdfOCRTagging) && app.pdfOCRCompleteTag != "" {
			// Add the OCR complete tag if tagging is enabled
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
				var partial *PartialUpdateError
				if errors.As(err, &partial) {
					applyFailTagAfterPartialSuccess(ctx, app.Client, app.Database, partial.DocumentID, partial.DroppedFields)
					// Treat as a (partial) success: tag was removed, fail tag applied.
					docLogger.Info("Successfully processed document OCR (with partial-update fail-tag marker)")
					successCount++
					continue
				}
				docLogger.Errorf("Update after OCR failed: %v", err)
				errs = append(errs, fmt.Errorf("document %d update error: %w", document.ID, err))
				recoverFromFailedUpdate(ctx, app.Client, app.Database, document, autoOcrTag)
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
