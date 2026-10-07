package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paperless-gpt/ocr"

	"github.com/gardar/ocrchestra/pkg/hocr"
	"github.com/gardar/ocrchestra/pkg/pdfocr"
	"github.com/sirupsen/logrus"
)

// ocrSupportsPromptOverride reports whether the configured provider honors a
// per-run prompt (only the LLM provider does). Used to reject overrides that
// would otherwise be silently ignored.
func (app *App) ocrSupportsPromptOverride() bool {
	_, ok := app.ocrProvider.(*ocr.LLMProvider)
	return ok
}

// replaceAfterUploadError signals that the searchable PDF was uploaded
// successfully but deleting the original document (replace mode) then failed.
// The upload must not be reported as failed — a retry would create a duplicate.
type replaceAfterUploadError struct{ cause error }

func (e *replaceAfterUploadError) Error() string {
	return "PDF uploaded but original could not be deleted: " + e.cause.Error()
}
func (e *replaceAfterUploadError) Unwrap() error { return e.cause }

// ProcessedDocument represents a document after OCR processing
type ProcessedDocument struct {
	ID               int
	Text             string
	HOCRStruct       *hocr.HOCR
	HOCR             string
	PDFData          []byte
	ReplacedOriginal bool   // true when the original document was successfully deleted and replaced
	PDFAction        string // "none", "attached", "versioned", "replaced", "skipped", "failed" — what happened to the searchable PDF
	PDFDetail        string // human-readable reason for "skipped"/"failed"
}

// HOCRCapable defines an interface for OCR providers that can generate hOCR
type HOCRCapable interface {
	// IsHOCREnabled returns whether hOCR generation is enabled
	IsHOCREnabled() bool

	// GetHOCRPages returns all hOCR pages collected during processing
	GetHOCRPages() []hocr.Page

	// GetHOCRDocument returns the complete hOCR document structure
	GetHOCRDocument() (*hocr.HOCR, error)

	// ResetHOCR clears any stored hOCR data
	ResetHOCR()
}

// ProcessDocumentOCR processes a document through OCR and returns the combined text, hOCR and PDF
func (app *App) ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions, jobID string) (*ProcessedDocument, error) {
	// Validate options for safety
	if !options.UploadPDF && options.ReplaceOriginal {
		return nil, fmt.Errorf("invalid OCROptions: cannot set ReplaceOriginal=true when UploadPDF=false")
	}
	if options.UploadMode == PDFUploadModeVersion && options.ReplaceOriginal {
		return nil, fmt.Errorf("invalid OCROptions: cannot set ReplaceOriginal=true when UploadMode=%s", PDFUploadModeVersion)
	}

	docLogger := documentLogger(documentID)
	if jobID != "" {
		docLogger = docLogger.WithField("job_id", jobID)
	}
	docLogger.Info("Starting OCR processing")

	// Render OCR prompt per-document with existing content (same pattern as title/tag/etc. prompts).
	// A run-scoped Prompt Override takes precedence over the saved template.
	// Use a call-scoped provider clone to avoid mutating the shared singleton.
	provider := app.ocrProvider
	var ocrPrompt string
	var err error
	if options.PromptOverride != "" {
		ocrPrompt, err = renderOCRPromptOverride(options.PromptOverride, options.ExistingContent)
		if err != nil {
			// An override the user explicitly typed must not silently fall back.
			return nil, fmt.Errorf("prompt override failed to render: %w", err)
		}
	} else {
		ocrPrompt, err = renderOCRPrompt(options.ExistingContent)
		if err != nil {
			docLogger.WithError(err).Warn("Failed to render per-document OCR prompt, using provider default")
			ocrPrompt = ""
		}
	}
	if ocrPrompt != "" {
		if llmProv, ok := provider.(*ocr.LLMProvider); ok {
			provider = llmProv.WithPrompt(ocrPrompt)
		}
	}

	// Determine the actual process mode to use
	processMode := options.ProcessMode
	if processMode == "" {
		processMode = app.ocrProcessMode
	} else if processMode != "image" && processMode != "pdf" && processMode != "whole_pdf" {
		return nil, fmt.Errorf("invalid ProcessMode: %s, must be one of: image, pdf, whole_pdf", processMode)
	}

	// Skip OCR if PDF already has OCR
	if app.pdfSkipExistingOCR && (processMode == "pdf" || processMode == "whole_pdf") {
		docLogger.Infof("Checking for existing OCR in PDF document (mode: %s)...", processMode)

		document, err := app.Client.GetDocument(ctx, documentID)
		if err != nil {
			return nil, fmt.Errorf("error fetching document %d: %w", documentID, err)
		}

		// Skip OCR if the document already has the OCR complete tag
		if app.pdfOCRTagging {
			for _, tag := range document.Tags {
				if tag == app.pdfOCRCompleteTag {
					docLogger.Infof("Document already has OCR complete tag '%s', skipping OCR processing", app.pdfOCRCompleteTag)
					return &ProcessedDocument{
						ID:   documentID,
						Text: document.Content,
					}, nil
				}
			}
		}

		// Then check if PDF has OCR layers (if enabled and applicable mode)
		if app.pdfSkipExistingOCR && (processMode == "pdf" || processMode == "whole_pdf") {
			docLogger.Infof("Checking for existing OCR in PDF document (mode: %s)...", processMode)

			// Download the PDF to check for OCR layers
			_, pdfBytes, _, err := app.Client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
			if err != nil {
				docLogger.Warnf("Failed to download PDF for OCR detection: %v, continuing with OCR process", err)
			} else {
				// Configure pdfocr with Strict mode
				pdfConfig := pdfocr.DefaultConfig()
				pdfConfig.Strict = true

				// Use pdfocr to detect existing OCR
				ocrResult, err := pdfocr.DetectOCR(pdfBytes, pdfConfig)
				if err != nil {
					docLogger.Warnf("OCR detection error: %v, continuing with OCR process", err)
				} else if ocrResult.HasOCR || ocrResult.HasLayerOCR {
					docLogger.Infof("⚠️ Skipping OCR processing - detected existing OCR layers in PDF")
					return &ProcessedDocument{
						ID:   documentID,
						Text: document.Content,
					}, nil
				}
			}
		}
	}
	// Check if we have an hOCR-capable provider
	var hocrCapable HOCRCapable
	var hasHOCR bool

	hocrCapable, hasHOCR = provider.(HOCRCapable)

	// Reset hOCR if the provider supports it
	if hasHOCR {
		hocrCapable.ResetHOCR()
	} else {
		docLogger.Debug("OCR provider does not support hOCR")
	}

	// Run options are resolved against saved/env defaults before processing.
	// Keep an explicit zero here: it means process every page.
	pageLimit := options.LimitPages

	var ocrTexts []string
	var imageDataList [][]byte
	var originalPDFData []byte
	var totalPdfPages int
	var imagePaths []string
	var ocrResults []*ocr.OCRResult

	// Default process mode to app's ocrProcessMode if not set in options
	processMode = options.ProcessMode
	if processMode == "" {
		processMode = app.ocrProcessMode
	}

	if processMode == "whole_pdf" {
		// Process the entire PDF in one go, skipping the splitting step
		var pdfBytes []byte
		var err error
		_, pdfBytes, totalPdfPages, err = app.Client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
		if err != nil {
			return nil, fmt.Errorf("error downloading document PDF for document %d: %w", documentID, err)
		}

		// Store the PDF data in the outer variable
		originalPDFData = pdfBytes

		// Record the page total so progress reporting has a denominator
		// (whole_pdf reports a single step, but the UI still divides by it).
		if jobID != "" {
			jobStore.Lock()
			if job, exists := jobStore.jobs[jobID]; exists {
				job.TotalPages = totalPdfPages
			}
			jobStore.Unlock()
		}

		docLogger.WithFields(logrus.Fields{
			"pdf_size":         len(originalPDFData),
			"total_page_count": totalPdfPages,
		}).Debug("Processing whole PDF document")

		// Process the whole PDF in one go
		result, err := provider.ProcessImage(ctx, originalPDFData, 0) // Page 0 indicates entire document
		if err != nil {
			return nil, fmt.Errorf("error performing OCR for document %d: %w", documentID, err)
		}

		if result == nil {
			docLogger.Error("Got nil result from OCR provider")
			return nil, fmt.Errorf("error performing OCR for document %d: nil result", documentID)
		}

		docLogger.WithField("content_length", len(result.Text)).
			WithField("has_hocr_page", result.HOCRPage != nil).
			Debug("OCR completed for full document")

		ocrTexts = append(ocrTexts, result.Text)

		// whole_pdf yields one combined text; store it as a single page result
		// so the Playground can show and compare it like any other run.
		var genInfoJSON string
		if result.GenerationInfo != nil {
			if b, err := json.Marshal(result.GenerationInfo); err == nil {
				genInfoJSON = string(b)
			}
		}
		if saveErr := SaveSingleOcrPageResult(app.Database, documentID, jobID, 0, result.Text, result.OcrLimitHit, genInfoJSON); saveErr != nil {
			docLogger.WithError(saveErr).Error("Failed to save OCR result to database")
		}
		if jobID != "" {
			jobStore.updatePagesDone(jobID, totalPdfPages)
		}
	} else if processMode == "pdf" {
		// Process PDF pages individually
		pdfPaths, pdfData, pdfPageCount, err := app.Client.DownloadDocumentAsPDF(ctx, documentID, pageLimit, true)
		defer func() {
			for _, pdfPath := range pdfPaths {
				if err := os.Remove(pdfPath); err != nil {
					docLogger.WithError(err).WithField("pdf_path", pdfPath).Warn("Failed to remove temporary PDF file")
				}
			}
		}()
		if err != nil {
			return nil, fmt.Errorf("error downloading document PDFs for document %d: %w", documentID, err)
		}

		// Store the original PDF data
		originalPDFData = pdfData
		totalPdfPages = pdfPageCount

		if jobID != "" {
			jobStore.Lock()
			if job, exists := jobStore.jobs[jobID]; exists {
				job.TotalPages = totalPdfPages
			}
			jobStore.Unlock()
		}

		// Log the page count information
		docLogger.WithFields(logrus.Fields{
			"processed_page_count": len(pdfPaths),
			"total_page_count":     totalPdfPages,
			"limit_pages":          pageLimit,
		}).Debug("Downloaded document PDFs")

		for i, pdfPath := range pdfPaths {
			pageLogger := docLogger.WithField("page", i+1)
			pageLogger.Debug("Processing page")

			pdfContent, err := os.ReadFile(pdfPath)
			if err != nil {
				return nil, fmt.Errorf("error reading PDF file for document %d, page %d: %w", documentID, i+1, err)
			}

			// Pass the page number (1-based index) to ProcessImage
			result, err := provider.ProcessImage(ctx, pdfContent, i+1)
			if err != nil {
				return nil, fmt.Errorf("error performing OCR for document %d, page %d: %w", documentID, i+1, err)
			}
			if result == nil {
				pageLogger.Error("Got nil result from OCR provider")
				return nil, fmt.Errorf("error performing OCR for document %d, page %d: nil result", documentID, i+1)
			}

			pageLogger.WithField("has_hocr_page", result.HOCRPage != nil).
				WithField("metadata", result.Metadata).
				Debug("OCR completed for page")

			ocrTexts = append(ocrTexts, result.Text)

			if jobID != "" {
				jobStore.updatePagesDone(jobID, i+1)
			}

			var genInfoJSON string
			if result.GenerationInfo != nil {
				if b, err := json.Marshal(result.GenerationInfo); err == nil {
					genInfoJSON = string(b)
				}
			}
			if saveErr := SaveSingleOcrPageResult(app.Database, documentID, jobID, i, result.Text, result.OcrLimitHit, genInfoJSON); saveErr != nil {
				pageLogger.WithError(saveErr).Error("Failed to save OCR page result to database")
			}
		}
	} else {
		// Process pages as images
		imagePaths, imgPageCount, err := app.Client.DownloadDocumentAsImages(ctx, documentID, pageLimit)
		defer func() {
			for _, imagePath := range imagePaths {
				if err := os.Remove(imagePath); err != nil {
					docLogger.WithError(err).WithField("image_path", imagePath).Warn("Failed to remove temporary image file")
				}
			}
		}()
		if err != nil {
			return nil, fmt.Errorf("error downloading document images for document %d: %w", documentID, err)
		}

		totalPdfPages = imgPageCount

		if jobID != "" {
			jobStore.Lock()
			if job, exists := jobStore.jobs[jobID]; exists {
				job.TotalPages = totalPdfPages
			}
			jobStore.Unlock()
		}

		// Log the page count information
		docLogger.WithFields(logrus.Fields{
			"processed_page_count": len(imagePaths),
			"total_page_count":     totalPdfPages,
			"limit_pages":          pageLimit,
		}).Debug("Downloaded document images")

		for i, imagePath := range imagePaths {
			select {
			case <-ctx.Done():
				docLogger.Info("Job cancelled before processing page")
				// Return partial results if cancelled
				return &ProcessedDocument{
					ID:   documentID,
					Text: strings.Join(ocrTexts, "\n\n"),
				}, ctx.Err()
			default:
			}

			pageLogger := docLogger.WithField("page", i+1)
			pageLogger.Debug("Processing page")

			imageContent, err := os.ReadFile(imagePath)
			if err != nil {
				return nil, fmt.Errorf("error reading image file for document %d, page %d: %w", documentID, i+1, err)
			}

			// Store image data for potential PDF generation
			imageDataList = append(imageDataList, imageContent)

			// Pass the page number (1-based index) to ProcessImage
			result, err := provider.ProcessImage(ctx, imageContent, i+1)
			if err != nil {
				return nil, fmt.Errorf("error performing OCR for document %d, page %d: %w", documentID, i+1, err)
			}
			if result == nil {
				pageLogger.Error("Got nil result from OCR provider")
				return nil, fmt.Errorf("error performing OCR for document %d, page %d: nil result", documentID, i+1)
			}

			if jobID != "" {
				jobStore.updatePagesDone(jobID, i+1)
			}

			pageLogger.WithField("has_hocr_page", result.HOCRPage != nil).
				WithField("metadata", result.Metadata).
				Debug("OCR completed for page")

			ocrTexts = append(ocrTexts, result.Text)
			ocrResults = append(ocrResults, result)

			var genInfoJSON string
			if result.GenerationInfo != nil {
				if b, err := json.Marshal(result.GenerationInfo); err == nil {
					genInfoJSON = string(b)
				}
			}

			saveErr := SaveSingleOcrPageResult(app.Database, documentID, jobID, i, result.Text, result.OcrLimitHit, genInfoJSON)
			if saveErr != nil {
				pageLogger.WithError(saveErr).Error("Failed to save OCR page result to database")
				// Continue processing other pages even if saving fails for one
			}
		}
	}

	fullText := strings.Join(ocrTexts, "\n\n")

	// Create ProcessedDocument to hold all the results
	processedDoc := &ProcessedDocument{
		ID:        documentID,
		Text:      fullText,
		PDFAction: "none",
	}

	if options.UploadPDF && !hasHOCR {
		processedDoc.PDFAction = "skipped"
		processedDoc.PDFDetail = "The configured OCR provider does not produce hOCR; searchable PDFs need an hOCR-capable provider."
		docLogger.Warn("Searchable PDF requested but provider is not hOCR-capable")
	}

	// Generate complete hOCR if we have hOCR capability
	if hasHOCR {
		hocrDoc, err := hocrCapable.GetHOCRDocument()
		if err == nil && hocrDoc != nil {
			// Store the hOCR struct in the processed document
			processedDoc.HOCRStruct = hocrDoc

			// Generate the HTML from the complete document
			hOCR, err := hocr.GenerateHOCRDocument(hocrDoc)
			if err == nil {
				docLogger.WithField("page_count", len(hocrCapable.GetHOCRPages())).
					Info("Successfully generated hOCR document")

				// Store the HTML in the processed document
				processedDoc.HOCR = hOCR

				// Save the hOCR to a file if enabled
				if app.createLocalHOCR && app.localHOCRPath != "" {
					if err := app.saveHOCRToFile(documentID, hOCR); err != nil {
						docLogger.WithError(err).Error("Failed to save hOCR file")
					} else {
						docLogger.Info("Successfully saved hOCR file")
					}
				}

				// Generate the searchable PDF when a local copy is configured or
				// this run wants to upload one. (Historically this was gated on
				// CREATE_LOCAL_PDF alone; per-run uploads must not depend on it.)
				wantLocalPDF := app.createLocalPDF && app.localPDFPath != ""
				if wantLocalPDF || options.UploadPDF {
					var processedPageCount int
					if processMode == "pdf" || processMode == "whole_pdf" {
						processedPageCount = len(ocrTexts)
					} else {
						processedPageCount = len(imagePaths)
					}

					// SAFETY CHECK: Don't generate PDF if we're processing fewer pages than original document
					if processedPageCount != totalPdfPages {
						docLogger.WithFields(logrus.Fields{
							"processed_pages": processedPageCount,
							"total_pages":     totalPdfPages,
							"limit":           pageLimit,
							"process_mode":    processMode,
						}).Warn("Not generating PDF because fewer pages were processed than exist in the original document")
						if options.UploadPDF {
							processedDoc.PDFAction = "skipped"
							processedDoc.PDFDetail = fmt.Sprintf("Only %d of %d pages were processed (page limit); a searchable PDF needs the whole document.", processedPageCount, totalPdfPages)
						}
					} else {
						docLogger.Info("Applying OCR to PDF")

						// Set up PDF configuration
						pdfConfig := pdfocr.DefaultConfig()

						var pdfData []byte
						var err error

						// For both "pdf" and "whole_pdf" modes, use ApplyOCR with original PDF data.
						// pdfocr.ApplyOCR transitively calls into gofpdi which can panic on
						// malformed PDFs (#945). Recover so the worker keeps draining the queue.
						applyOCR := func() (data []byte, err error) {
							defer func() {
								if r := recover(); r != nil {
									err = fmt.Errorf("apply OCR panicked: %v", r)
								}
							}()
							if (processMode == "pdf" || processMode == "whole_pdf") && originalPDFData != nil {
								docLogger.Debug("Using ApplyOCR with original PDF data")
								return pdfocr.ApplyOCR(originalPDFData, hocrDoc, pdfConfig)
							}
							if len(imageDataList) > 0 {
								docLogger.Debug("Using AssembleWithOCR with image data")
								return pdfocr.AssembleWithOCR(hocrDoc, imageDataList, pdfConfig)
							}
							return nil, fmt.Errorf("no suitable data available for PDF generation")
						}
						pdfData, err = applyOCR()

						if err != nil {
							docLogger.WithError(err).Error("Failed to apply OCR to PDF")
							if options.UploadPDF {
								processedDoc.PDFAction = "failed"
								processedDoc.PDFDetail = fmt.Sprintf("PDF generation failed: %v", err)
							}
						} else {
							// Store PDF data in the processed document struct
							processedDoc.PDFData = pdfData

							// Save the PDF to a file when a local copy is configured
							if wantLocalPDF {
								if err := app.savePDFToFile(ctx, documentID, pdfData); err != nil {
									docLogger.WithError(err).Error("Failed to save PDF file")
								} else {
									docLogger.Info("Successfully generated and saved PDF")
								}
							}

							// Upload PDF to paperless-ngx if requested
							if options.UploadPDF && pdfData != nil {
								err := app.uploadProcessedPDF(ctx, documentID, pdfData, options, docLogger)
								var replaceErr *replaceAfterUploadError
								var unconfirmedErr *versionUnconfirmedError
								switch {
								case err == nil && options.UploadMode == PDFUploadModeVersion:
									processedDoc.PDFAction = "versioned"
								case errors.As(err, &unconfirmedErr):
									processedDoc.PDFAction = "versioned"
									processedDoc.PDFDetail = fmt.Sprintf("Uploaded, but paperless-ngx had not confirmed the new version when paperless-gpt stopped waiting; check task %s in paperless-ngx.", unconfirmedErr.taskID)
								case err == nil && options.ReplaceOriginal:
									processedDoc.ReplacedOriginal = true
									processedDoc.PDFAction = "replaced"
								case err == nil:
									processedDoc.PDFAction = "attached"
								case errors.As(err, &replaceErr):
									// The searchable PDF was uploaded; only deleting the
									// original failed. Report it as attached-with-warning
									// rather than "failed" so the user isn't misled into a
									// retry that would create a duplicate.
									docLogger.WithError(err).Error("PDF uploaded but original could not be replaced")
									processedDoc.PDFAction = "attached"
									processedDoc.PDFDetail = fmt.Sprintf("Searchable PDF was uploaded, but the original could not be deleted: %v", replaceErr.cause)
								default:
									docLogger.WithError(err).Error("Failed to upload processed PDF")
									processedDoc.PDFAction = "failed"
									processedDoc.PDFDetail = fmt.Sprintf("PDF upload failed: %v", err)
								}
							}
						}
					}
				}
			} else {
				docLogger.WithError(err).Error("Failed to generate hOCR")
				if options.UploadPDF {
					processedDoc.PDFAction = "failed"
					processedDoc.PDFDetail = fmt.Sprintf("hOCR generation failed: %v", err)
				}
			}
		} else if err != nil {
			docLogger.WithError(err).Error("Failed to create hOCR document")
			if options.UploadPDF {
				processedDoc.PDFAction = "failed"
				processedDoc.PDFDetail = fmt.Sprintf("hOCR document creation failed: %v", err)
			}
		}
	}

	docLogger.Info("OCR processing completed successfully")
	return processedDoc, nil
}

// saveHOCRToFile saves the hOCR HTML to a file
// TODO: Implement a proper solution to store this alongside the document in Paperless
func (app *App) saveHOCRToFile(documentID int, hOCR string) error {
	// Ensure the directory exists
	if err := os.MkdirAll(app.localHOCRPath, 0755); err != nil {
		return fmt.Errorf("failed to create HOCR output directory: %w", err)
	}

	// Create the file path
	filename := fmt.Sprintf("%08d_paperless-gpt_ocr.hocr", documentID)
	filePath := filepath.Join(app.localHOCRPath, filename)

	// Write the HOCR to the file
	if err := os.WriteFile(filePath, []byte(hOCR), 0644); err != nil {
		return fmt.Errorf("failed to write HOCR file: %w", err)
	}

	return nil
}

// savePDFToFile saves the PDF data to a file
func (app *App) savePDFToFile(ctx context.Context, documentID int, pdfData []byte) error {
	// Ensure the directory exists
	if err := os.MkdirAll(app.localPDFPath, 0755); err != nil {
		return fmt.Errorf("failed to create PDF output directory: %w", err)
	}

	// Always use PDF extension for generated PDFs
	filename := fmt.Sprintf("%08d_paperless-gpt_ocr.pdf", documentID)

	// Create the file path
	filePath := filepath.Join(app.localPDFPath, filename)

	// Write the PDF to the file
	if err := os.WriteFile(filePath, pdfData, 0644); err != nil {
		return fmt.Errorf("failed to write PDF file: %w", err)
	}

	return nil
}

// Upload PDF to Paperless
func (app *App) uploadProcessedPDF(ctx context.Context, documentID int, pdfData []byte, options OCROptions, logger *logrus.Entry) error {
	if options.UploadMode == PDFUploadModeVersion {
		return app.uploadProcessedPDFAsVersion(ctx, documentID, pdfData, logger)
	}

	// Get the original document metadata
	originalDoc, err := app.Client.GetDocument(ctx, documentID)
	if err != nil {
		return fmt.Errorf("error fetching original document: %w", err)
	}

	// Always use PDF extension for generated PDFs
	filename := fmt.Sprintf("%08d_paperless-gpt_ocr.pdf", documentID)

	// Prepare metadata for the upload
	metadata := map[string]interface{}{
		"title": originalDoc.Title,
	}

	// Copy metadata from original document if requested
	if options.CopyMetadata {
		// Get tag IDs
		allTags, err := app.Client.GetAllTags(ctx)
		if err == nil {
			var tagIDs []int
			for _, tagName := range originalDoc.Tags {
				if tagID, ok := allTags[tagName]; ok {
					tagIDs = append(tagIDs, tagID)
				}
			}

			// Add or create the OCR complete tag if tagging is enabled
			if app.pdfOCRTagging {
				if tagID, ok := allTags[app.pdfOCRCompleteTag]; ok {
					tagIDs = append(tagIDs, tagID)
				} else {
					// Create the tag if it doesn't exist
					tagID, err := app.Client.CreateTag(ctx, app.pdfOCRCompleteTag)
					if err == nil {
						tagIDs = append(tagIDs, tagID)
					} else {
						logger.WithError(err).Warn("Could not create OCR complete tag")
					}
				}
			}

			if len(tagIDs) > 0 {
				metadata["tags"] = tagIDs
			}
		}

		// Get correspondent ID
		if originalDoc.Correspondent != "" {
			allCorrespondents, err := app.Client.GetAllCorrespondents(ctx)
			if err == nil {
				if correspondentID, ok := allCorrespondents[originalDoc.Correspondent]; ok {
					metadata["correspondent"] = correspondentID
				}
			}
		}

		// Set created date if available
		if originalDoc.CreatedDate != "" {
			metadata["created"] = originalDoc.CreatedDate
		}
	} else if app.pdfOCRTagging {
		// Even if not copying all metadata, still add the OCR complete tag if tagging is enabled
		allTags, err := app.Client.GetAllTags(ctx)
		if err == nil {
			if tagID, ok := allTags[app.pdfOCRCompleteTag]; ok {
				metadata["tags"] = []int{tagID}
			} else {
				// Create the tag if it doesn't exist
				tagID, err := app.Client.CreateTag(ctx, app.pdfOCRCompleteTag)
				if err == nil {
					metadata["tags"] = []int{tagID}
				} else {
					logger.WithError(err).Warn("Could not create OCR complete tag")
				}
			}
		}
	}

	// Upload the PDF
	logger.WithField("filename", filename).Info("Uploading processed PDF to Paperless-ngx")
	taskID, err := app.Client.UploadDocument(ctx, pdfData, filename, metadata)
	if err != nil {
		return fmt.Errorf("error uploading PDF: %w", err)
	}

	logger.WithField("task_id", taskID).Info("PDF uploaded successfully")

	// If replacing the original is requested, delete it after upload, but
	// only once paperless-ngx has confirmed that the upload was imported.
	// Deleting on anything less (status unreadable, still pending when we stop
	// waiting) can leave neither the original nor the replacement.
	if options.ReplaceOriginal {
		logger.Info("Waiting for document processing to complete before deletion...")
		imported, reason, err := app.waitForImport(ctx, taskID, logger)
		if err != nil {
			return err
		}
		if !imported {
			return &replaceAfterUploadError{cause: fmt.Errorf(
				"paperless-ngx did not confirm the import of the searchable PDF (task %s: %s), so the original was kept; delete it once the new document shows up", taskID, reason)}
		}

		// Delete original document. The PDF is already uploaded at this point,
		// so signal that distinctly: the caller must not report the whole upload
		// as failed (a retry would create a duplicate).
		if err := app.Client.DeleteDocument(ctx, documentID); err != nil {
			return &replaceAfterUploadError{cause: err}
		}
		logger.Info("Original document deleted successfully")
	}

	return nil
}

// waitForImport polls an upload task until paperless-ngx reports it done.
// imported is true only for a SUCCESS status. A FAILURE status is returned as
// an error; anything else (status unreadable, still pending when the wait
// ends) returns imported=false with the reason, so the caller can keep data
// it would otherwise delete.
func (app *App) waitForImport(ctx context.Context, taskID string, logger *logrus.Entry) (imported bool, reason string, err error) {
	reason = "still pending"
	for attempt := 0; attempt < taskPollAttempts; attempt++ {
		task, statusErr := app.Client.GetTaskStatus(ctx, taskID)
		if statusErr != nil {
			reason = fmt.Sprintf("status unavailable: %v", statusErr)
		} else {
			status, _ := task["status"].(string)
			switch {
			case strings.EqualFold(status, "SUCCESS"):
				logger.Info("Document processing completed successfully")
				return true, "", nil
			case strings.EqualFold(status, "FAILURE"):
				return false, "", fmt.Errorf("document processing failed, not deleting original document: %v", taskResultDetail(task))
			case status == "":
				reason = "status unknown"
			default:
				reason = "still " + strings.ToLower(status)
			}
		}
		if attempt < taskPollAttempts-1 {
			logger.Infof("Document not imported yet (%s), waiting %v before checking again", reason, taskPollInterval)
			select {
			case <-ctx.Done():
				return false, ctx.Err().Error(), nil
			case <-time.After(taskPollInterval):
			}
		}
	}
	logger.Warnf("paperless-ngx did not confirm the import (%s); keeping the original document", reason)
	return false, reason, nil
}

// uploadProcessedPDFAsVersion adds the searchable PDF as a new version of the
// same document. Unlike a new-document upload there is no metadata to copy and
// nothing to delete: tags, custom fields, notes and the id stay on the document,
// and paperless-ngx keeps the original file as the previous version.
func (app *App) uploadProcessedPDFAsVersion(ctx context.Context, documentID int, pdfData []byte, logger *logrus.Entry) error {
	filename := fmt.Sprintf("%08d_paperless-gpt_ocr.pdf", documentID)
	taskID, err := app.Client.UploadDocumentVersion(ctx, documentID, pdfData, filename, "paperless-gpt OCR")
	if err != nil {
		return fmt.Errorf("error uploading PDF as new version: %w", err)
	}
	taskLogger := logger.WithField("task_id", taskID)
	taskLogger.Info("Searchable PDF uploaded; waiting for paperless-ngx to add it as a new version")

	// paperless-ngx only queues the import. Report a rejected import as a
	// failure instead of as a new version that never appears. An unknown
	// outcome (status unreadable, still pending) is not a failure: the upload
	// was accepted and retrying would add a second version.
	var lastErr error
	for attempt := 0; attempt < taskPollAttempts; attempt++ {
		task, err := app.Client.GetTaskStatus(ctx, taskID)
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
			status, _ := task["status"].(string)
			switch {
			case strings.EqualFold(status, "SUCCESS"):
				taskLogger.Info("paperless-ngx added the searchable PDF as a new version")
				return nil
			case strings.EqualFold(status, "FAILURE"):
				return fmt.Errorf("paperless-ngx rejected the new version: %v", taskResultDetail(task))
			}
		}
		if attempt < taskPollAttempts-1 {
			select {
			case <-ctx.Done():
				return &versionUnconfirmedError{taskID: taskID, cause: ctx.Err()}
			case <-time.After(taskPollInterval):
			}
		}
	}
	if lastErr != nil {
		taskLogger.WithError(lastErr).Warn("Could not check whether paperless-ngx added the new version")
	} else {
		taskLogger.Warn("paperless-ngx has not finished adding the new version; not waiting longer")
	}
	return &versionUnconfirmedError{taskID: taskID, cause: lastErr}
}

// versionUnconfirmedError means the searchable PDF was accepted for import as a
// new version, but paperless-ngx had not confirmed it when paperless-gpt stopped
// waiting. It is not a failure: retrying would add a second version.
type versionUnconfirmedError struct {
	taskID string
	cause  error
}

func (e *versionUnconfirmedError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("paperless-ngx has not confirmed the new version (task %s): %v", e.taskID, e.cause)
	}
	return fmt.Sprintf("paperless-ngx has not confirmed the new version yet (task %s)", e.taskID)
}

func (e *versionUnconfirmedError) Unwrap() error { return e.cause }

// Waiting on paperless-ngx to import a new version; tests shorten these.
var (
	taskPollAttempts = 12
	taskPollInterval = 5 * time.Second
)

// taskResultDetail picks the most useful failure detail from a task object.
func taskResultDetail(task map[string]interface{}) interface{} {
	for _, key := range []string{"result_data", "result"} {
		if detail, ok := task[key]; ok && detail != nil {
			return detail
		}
	}
	return "no detail from paperless-ngx"
}
