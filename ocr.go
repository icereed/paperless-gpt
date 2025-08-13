package main

import (
	"context"
	"encoding/json"
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

// ProcessedDocument represents a document after OCR processing
type ProcessedDocument struct {
	ID         int
	Text       string
	HOCRStruct *hocr.HOCR
	HOCR       string
	PDFData    []byte
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

	docLogger := documentLogger(documentID)
	if jobID != "" {
		docLogger = docLogger.WithField("job_id", jobID)
	}
	docLogger.Info("Starting OCR processing")

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

	hocrCapable, hasHOCR = app.ocrProvider.(HOCRCapable)

	// Reset hOCR if the provider supports it
	if hasHOCR {
		hocrCapable.ResetHOCR()
	} else {
		docLogger.Debug("OCR provider does not support hOCR")
	}

	// Use the page limit from options if provided, otherwise use the global setting
	pageLimit := limitOcrPages
	if options.LimitPages > 0 {
		pageLimit = options.LimitPages
	}

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
		_, pdfBytes, totalPdfPages, err := app.Client.DownloadDocumentAsPDF(ctx, documentID, 0, false)
		if err != nil {
			return nil, fmt.Errorf("error downloading document PDF for document %d: %w", documentID, err)
		}

		// Store the PDF data in the outer variable
		originalPDFData = pdfBytes

		docLogger.WithFields(logrus.Fields{
			"pdf_size":         len(originalPDFData),
			"total_page_count": totalPdfPages,
		}).Debug("Processing whole PDF document")

		// Process the whole PDF in one go
		result, err := app.ocrProvider.ProcessImage(ctx, originalPDFData, 0) // Page 0 indicates entire document
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
			result, err := app.ocrProvider.ProcessImage(ctx, pdfContent, i+1)
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
			result, err := app.ocrProvider.ProcessImage(ctx, imageContent, i+1)
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

			saveErr := SaveSingleOcrPageResult(app.Database, documentID, i, result.Text, result.OcrLimitHit, genInfoJSON)
			if saveErr != nil {
				pageLogger.WithError(saveErr).Error("Failed to save OCR page result to database")
				// Continue processing other pages even if saving fails for one
			}
		}
	}

	fullText := strings.Join(ocrTexts, "\n\n")

	// Create ProcessedDocument to hold all the results
	processedDoc := &ProcessedDocument{
		ID:   documentID,
		Text: fullText,
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

				// Apply OCR to PDF if the feature is enabled
				if app.createLocalPDF && app.localPDFPath != "" {
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
					} else {
						docLogger.Info("Applying OCR to PDF")

						// Set up PDF configuration
						pdfConfig := pdfocr.DefaultConfig()

						var pdfData []byte
						var err error

						// For both "pdf" and "whole_pdf" modes, use ApplyOCR with original PDF data
						if (processMode == "pdf" || processMode == "whole_pdf") && originalPDFData != nil {
							docLogger.Debug("Using ApplyOCR with original PDF data")
							pdfData, err = pdfocr.ApplyOCR(originalPDFData, hocrDoc, pdfConfig)
						} else if len(imageDataList) > 0 {
							// Only for "image" mode, use AssembleWithOCR with image data
							docLogger.Debug("Using AssembleWithOCR with image data")
							pdfData, err = pdfocr.AssembleWithOCR(hocrDoc, imageDataList, pdfConfig)
						} else {
							docLogger.Error("No suitable data available for PDF generation")
							err = fmt.Errorf("no suitable data available for PDF generation")
						}

						if err != nil {
							docLogger.WithError(err).Error("Failed to apply OCR to PDF")
						} else {
							// Store PDF data in the processed document struct
							processedDoc.PDFData = pdfData

							// Save the PDF to a file
							if err := app.savePDFToFile(ctx, documentID, pdfData); err != nil {
								docLogger.WithError(err).Error("Failed to save PDF file")
							} else {
								docLogger.Info("Successfully generated and saved PDF")
							}

							// Upload PDF to paperless-ngx if requested
							if options.UploadPDF && pdfData != nil {
								if err := app.uploadProcessedPDF(ctx, documentID, pdfData, options, docLogger); err != nil {
									docLogger.WithError(err).Error("Failed to upload processed PDF")
								}
							}
						}
					}
				}
			} else {
				docLogger.WithError(err).Error("Failed to generate hOCR")
			}
		} else if err != nil {
			docLogger.WithError(err).Error("Failed to create hOCR document")
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

	// If replacing the original is requested, delete it after upload
	if options.ReplaceOriginal {
		// Poll for task completion
		maxRetries := 12
		waitTime := 5 * time.Second

		logger.Info("Waiting for document processing to complete before deletion...")

		for i := 0; i < maxRetries; i++ {
			taskStatus, err := app.Client.GetTaskStatus(ctx, taskID)
			if err != nil {
				logger.WithError(err).Warn("Failed to check task status, proceeding with deletion anyway")
				break
			}

			status, ok := taskStatus["status"].(string)
			if !ok {
				logger.Warn("Could not determine task status, proceeding with deletion anyway")
				break
			}

			if status == "SUCCESS" {
				logger.Info("Document processing completed successfully")
				break
			}

			if status == "FAILURE" {
				return fmt.Errorf("document processing failed, not deleting original document")
			}

			if i < maxRetries-1 {
				logger.Infof("Document still processing (status: %s), waiting %v before checking again", status, waitTime)
				time.Sleep(waitTime)
			}
		}

		// Delete original document
		if err := app.Client.DeleteDocument(ctx, documentID); err != nil {
			return fmt.Errorf("error deleting original document: %w", err)
		}
		logger.Info("Original document deleted successfully")
	}

	return nil
}
