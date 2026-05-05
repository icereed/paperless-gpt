package main

// Parser API (v1) — stub endpoints for a generic LLM-OCR HTTP service.
//
// See docs/parser_plugin_rfc.md for the full design. This API is intentionally
// consumer-agnostic: the headline use case is the paperless-ngx 3.0 parser
// plugin (loaded via a Python shim), but the same endpoints are designed to be
// equally useful from n8n / Make / Zapier workflows, local coding agents,
// CLI tools, and any RAG / document-ingestion pipeline.
//
// These handlers intentionally return 501 Not Implemented. They exist so the
// API surface can be reviewed and iterated on in the open before the full
// implementation lands. Do NOT enable in production yet.

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ParserProviderInfo describes one available OCR/LLM provider for the
// /capabilities response. Consumer UIs (e.g. n8n) use this to populate
// dropdowns; the paperless-ngx shim uses it to decide ParserProtocol.score().
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
}

// ParserMetadataEntry mirrors paperless-ngx's MetadataEntry TypedDict but is
// also a useful generic shape for any consumer that wants structured
// per-document metadata. All values are stringified before being returned.
type ParserMetadataEntry struct {
	Namespace string `json:"namespace"`
	Prefix    string `json:"prefix"`
	Key       string `json:"key"`
	Value     string `json:"value"`
}

// ParseResponse is the response body of POST /api/v1/parse.
//
// Binary fields (archive PDF, thumbnail) are base64-encoded in the JSON body
// for ease of consumption from any HTTP client. If bandwidth becomes an issue
// for large PDFs we can revisit with multipart/mixed; see RFC open questions.
type ParseResponse struct {
	Text             string                `json:"text"`
	Date             *string               `json:"date,omitempty"`               // ISO-8601, nullable
	PageCount        *int                  `json:"page_count,omitempty"`         // nullable
	ArchivePDFB64    string                `json:"archive_pdf_b64,omitempty"`    // searchable PDF
	ThumbnailWebPB64 string                `json:"thumbnail_webp_b64,omitempty"` // WebP image
	Metadata         []ParserMetadataEntry `json:"metadata,omitempty"`
	Provider         string                `json:"provider,omitempty"`      // which OCR provider handled it
	OCRLimitHit      bool                  `json:"ocr_limit_hit,omitempty"` // mirrors ocr.OCRResult.OcrLimitHit
}

// notImplemented returns a 501 with a pointer to the RFC. Used by every stub.
func notImplemented(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":     "not yet implemented",
		"see":       "docs/parser_plugin_rfc.md",
		"discussion": "https://github.com/icereed/paperless-gpt/pull/<this PR>",
	})
}

// parserCapabilitiesHandler handles GET /api/v1/capabilities.
//
// TODO(parser-plugin): populate from app.ocrProvider and the active config.
func (app *App) parserCapabilitiesHandler(c *gin.Context) {
	notImplemented(c)
}

// parserParseHandler handles POST /api/v1/parse.
//
// Expected request: multipart/form-data with fields
//   {file, mime_type?, filename?, produce_archive?, produce_thumbnail?,
//    produce_text?, provider?, language_hint?, context?}
//
// TODO(parser-plugin): refactor ocr.ProcessDocumentOCR so it can be called
// without a paperless-ngx document ID, then wire it up here.
func (app *App) parserParseHandler(c *gin.Context) {
	notImplemented(c)
}

// parserHealthzHandler handles GET /api/v1/healthz.
//
// TODO(parser-plugin): probe the configured OCR provider.
func (app *App) parserHealthzHandler(c *gin.Context) {
	notImplemented(c)
}

// registerParserAPI mounts the v1 parse endpoints. Endpoints are always
// mounted (so the shape can be inspected via the running server) but currently
// return 501.
func (app *App) registerParserAPI(router *gin.Engine) {
	v1 := router.Group("/api/v1")
	{
		v1.GET("/capabilities", app.parserCapabilitiesHandler)
		v1.POST("/parse", app.parserParseHandler)
		v1.GET("/healthz", app.parserHealthzHandler)
	}
}
