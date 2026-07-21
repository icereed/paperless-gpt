package main

// Parser API (v1) — generic LLM-OCR HTTP service.
//
// Headline use case: paperless-ngx 3.0 parser plugin (loaded via the Python
// shim in paperless-gpt-parser/). The same endpoints are equally useful from
// n8n / Make / Zapier workflows, local coding agents that struggle with PDFs,
// CLI tools and RAG ingestion pipelines.
//
// See docs/parser_plugin_rfc.md for the full design. This file ships the
// MVP: text extraction for images and PDFs. Searchable archive PDF and
// thumbnail generation are intentionally deferred to a follow-up PR.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	"github.com/gen2brain/go-fitz"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Response shapes
// ---------------------------------------------------------------------------

// ParserProviderInfo describes one available OCR/LLM provider for the
// /capabilities response.
type ParserProviderInfo struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	CanProduceArchive bool   `json:"can_produce_archive"`
}

// ParserCapabilities is the response body of GET /api/v1/capabilities.
type ParserCapabilities struct {
	Name                 string               `json:"name"`
	Version              string               `json:"version"`
	SupportedMimeTypes   map[string]string    `json:"supported_mime_types"`
	Providers            []ParserProviderInfo `json:"providers"`
	DefaultProvider      string               `json:"default_provider"`
	CanProduceArchive    bool                 `json:"can_produce_archive"`
	RequiresPDFRendition bool                 `json:"requires_pdf_rendition"`
	DefaultScore         int                  `json:"default_score"`
	Notes                []string             `json:"notes,omitempty"`
}

// ParserMetadataEntry mirrors paperless-ngx's MetadataEntry TypedDict but is
// also a useful generic shape for any consumer that wants per-document
// structured metadata. All values are stringified.
type ParserMetadataEntry struct {
	Namespace string `json:"namespace"`
	Prefix    string `json:"prefix"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

// ParseResponse is the response body of POST /api/v1/parse.
//
// Binary fields (archive PDF, thumbnail) are base64-encoded for ease of
// consumption from any HTTP client. Empty strings indicate "not produced".
type ParseResponse struct {
	Text             string                `json:"text"`
	Date             *string               `json:"date,omitempty"`
	PageCount        *int                  `json:"page_count,omitempty"`
	ArchivePDFB64    string                `json:"archive_pdf_b64,omitempty"`
	ThumbnailWebPB64 string                `json:"thumbnail_webp_b64,omitempty"`
	Metadata         []ParserMetadataEntry `json:"metadata,omitempty"`
	Provider         string                `json:"provider,omitempty"`
	OCRLimitHit      bool                  `json:"ocr_limit_hit,omitempty"`
}

// ---------------------------------------------------------------------------
// MIME type allow-list
// ---------------------------------------------------------------------------

// supportedParseMimeTypes maps MIME types this server can handle to their
// preferred file extension. Same shape as paperless-ngx
// ParserProtocol.supported_mime_types() so the Python shim can pass it
// through unchanged.
var supportedParseMimeTypes = map[string]string{
	"application/pdf": ".pdf",
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/jpg":       ".jpg",
	"image/tiff":      ".tiff",
	"image/webp":      ".webp",
}

// ---------------------------------------------------------------------------
// Optional bearer-token auth
// ---------------------------------------------------------------------------

// requireAPIToken is a Gin middleware that enforces a bearer token when
// PAPERLESS_GPT_API_TOKEN is set. When unset (default), the API is open —
// suitable for trusted Docker networks.
func requireAPIToken() gin.HandlerFunc {
	expected := os.Getenv("PAPERLESS_GPT_API_TOKEN")
	return func(c *gin.Context) {
		if expected == "" {
			c.Next()
			return
		}
		auth := c.GetHeader("Authorization")
		if auth != "Bearer "+expected {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing or invalid bearer token",
			})
			return
		}
		c.Next()
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// parserCapabilitiesHandler handles GET /api/v1/capabilities.
func (app *App) parserCapabilitiesHandler(c *gin.Context) {
	if !app.isOcrEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OCR is not enabled on this paperless-gpt instance",
		})
		return
	}

	providerID := os.Getenv("OCR_PROVIDER")
	if providerID == "" {
		providerID = "llm"
	}

	resp := ParserCapabilities{
		Name:                 "paperless-gpt",
		Version:              version,
		SupportedMimeTypes:   supportedParseMimeTypes,
		Providers:            []ParserProviderInfo{{ID: providerID, DisplayName: providerID, CanProduceArchive: false}},
		DefaultProvider:      providerID,
		CanProduceArchive:    false, // MVP: not yet implemented in the v1 endpoint
		RequiresPDFRendition: false,
		DefaultScore:         50,
		Notes: []string{
			"MVP: text extraction only. Archive PDF and thumbnail are not yet returned.",
			"See docs/parser_plugin_rfc.md for the planned full surface.",
		},
	}
	c.JSON(http.StatusOK, resp)
}

// parserHealthzHandler handles GET /api/v1/healthz.
func (app *App) parserHealthzHandler(c *gin.Context) {
	if !app.isOcrEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "no-ocr-provider"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// parserParseHandler handles POST /api/v1/parse.
//
// Expected request: multipart/form-data with fields
//
//	file              — required, binary
//	mime_type         — optional, auto-detected if absent
//	filename          — optional, used for logging
//	produce_archive   — optional bool, currently ignored (MVP)
//	produce_thumbnail — optional bool, currently ignored (MVP)
//	produce_text      — optional bool, default true
//	provider          — optional, currently ignored (MVP, uses configured provider)
//	language_hint     — optional, currently ignored (MVP)
//	context           — optional JSON, opaque to the server
func (app *App) parserParseHandler(c *gin.Context) {
	if !app.isOcrEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "OCR is not enabled on this paperless-gpt instance",
		})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'file' field in multipart form"})
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot open uploaded file: %v", err)})
		return
	}
	defer f.Close()

	body, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot read uploaded file: %v", err)})
		return
	}
	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded file is empty"})
		return
	}

	mimeType := c.PostForm("mime_type")
	if mimeType == "" {
		mimeType = http.DetectContentType(body)
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))

	if _, ok := supportedParseMimeTypes[mimeType]; !ok {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{
			"error":                "mime type not supported",
			"mime_type":            mimeType,
			"supported_mime_types": supportedParseMimeTypes,
		})
		return
	}

	// Validate optional 'context' is parseable JSON if provided. Server treats
	// it as opaque, but we want to fail fast on garbage input.
	if rawCtx := c.PostForm("context"); rawCtx != "" {
		var probe any
		if err := json.Unmarshal([]byte(rawCtx), &probe); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("'context' must be valid JSON: %v", err),
			})
			return
		}
	}

	providerID := os.Getenv("OCR_PROVIDER")
	if providerID == "" {
		providerID = "llm"
	}

	ctx := c.Request.Context()
	logger := log.WithFields(logrus.Fields{
		"endpoint":   "POST /api/v1/parse",
		"mime_type":  mimeType,
		"filename":   fileHeader.Filename,
		"size_bytes": len(body),
	})
	logger.Info("Parse request received")

	var (
		text        string
		pageCount   int
		ocrLimitHit bool
	)

	if mimeType == "application/pdf" {
		text, pageCount, ocrLimitHit, err = app.parsePDFForAPI(ctx, body, logger)
	} else {
		// Image: pass the bytes straight to the provider as page 1.
		result, perr := app.ocrProvider.ProcessImage(ctx, body, 1)
		if perr != nil {
			err = perr
		} else if result == nil {
			err = fmt.Errorf("provider returned nil result")
		} else {
			text = result.Text
			pageCount = 1
			ocrLimitHit = result.OcrLimitHit
		}
	}

	if err != nil {
		logger.WithError(err).Error("Parse failed")
		// 503 (not 500) so callers know it's worth retrying or falling back
		// to a built-in parser.
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	pageCountPtr := &pageCount
	if pageCount == 0 {
		pageCountPtr = nil
	}

	c.JSON(http.StatusOK, ParseResponse{
		Text:        text,
		PageCount:   pageCountPtr,
		Provider:    providerID,
		OCRLimitHit: ocrLimitHit,
	})
}

// parsePDFForAPI splits a PDF into page images via go-fitz (already a
// dependency for the polling flow) and runs OCR per page. Mirrors the image
// preparation logic in paperless.go but operates entirely in-memory.
func (app *App) parsePDFForAPI(ctx context.Context, pdfBytes []byte, logger *logrus.Entry) (string, int, bool, error) {
	doc, err := fitz.NewFromMemory(pdfBytes)
	if err != nil {
		return "", 0, false, fmt.Errorf("opening PDF: %w", err)
	}
	defer doc.Close()

	totalPages := doc.NumPage()
	pagesToProcess := totalPages
	if limitOcrPages > 0 && limitOcrPages < totalPages {
		pagesToProcess = limitOcrPages
	}

	var (
		texts    []string
		limitHit bool
	)

	const minDPI = 72
	for n := 0; n < pagesToProcess; n++ {
		select {
		case <-ctx.Done():
			return strings.Join(texts, "\n\n"), totalPages, limitHit, ctx.Err()
		default:
		}

		rect, err := doc.Bound(n)
		if err != nil {
			return "", totalPages, limitHit, fmt.Errorf("bounding page %d: %w", n+1, err)
		}
		wPts := float64(rect.Dx())
		hPts := float64(rect.Dy())

		dpi := float64(minDPI)
		if imageMaxRenderDPI > 0 && imageMaxPixelDimension > 0 && imageMaxTotalPixels > 0 && wPts > 0 && hPts > 0 {
			dpiSide := float64(imageMaxPixelDimension*72) / math.Max(wPts, hPts)
			dpiArea := math.Sqrt(float64(imageMaxTotalPixels) * 72 * 72 / (wPts * hPts))
			dpi = math.Min(dpiSide, dpiArea)
			dpi = math.Min(dpi, float64(imageMaxRenderDPI))
			dpi = math.Max(dpi, minDPI)
		}

		img, err := doc.ImageDPI(n, dpi)
		if err != nil {
			return "", totalPages, limitHit, fmt.Errorf("rendering page %d: %w", n+1, err)
		}

		buf := &bytes.Buffer{}
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: jpeg.DefaultQuality}); err != nil {
			return "", totalPages, limitHit, fmt.Errorf("encoding page %d: %w", n+1, err)
		}

		result, err := app.ocrProvider.ProcessImage(ctx, buf.Bytes(), n+1)
		if err != nil {
			return "", totalPages, limitHit, fmt.Errorf("OCR page %d: %w", n+1, err)
		}
		if result == nil {
			return "", totalPages, limitHit, fmt.Errorf("OCR page %d: nil result", n+1)
		}
		texts = append(texts, result.Text)
		if result.OcrLimitHit {
			limitHit = true
		}
		logger.WithField("page", n+1).Debug("Page OCR completed")
	}
	return strings.Join(texts, "\n\n"), totalPages, limitHit, nil
}

// ---------------------------------------------------------------------------
// Router registration
// ---------------------------------------------------------------------------

// registerParserAPI mounts the v1 parse endpoints under /api/v1/.
func (app *App) registerParserAPI(router *gin.Engine) {
	v1 := router.Group("/api/v1", requireAPIToken())
	{
		v1.GET("/capabilities", app.parserCapabilitiesHandler)
		v1.POST("/parse", app.parserParseHandler)
		v1.GET("/healthz", app.parserHealthzHandler)
	}
}
