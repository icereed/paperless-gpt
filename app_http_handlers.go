package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

// getPromptsHandler handles the GET /api/prompts endpoint
func getPromptsHandler(c *gin.Context) {
	promptsDir := "prompts"
	files, err := os.ReadDir(promptsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not read prompts directory"})
		log.Errorf("Could not read prompts directory: %v", err)
		return
	}

	prompts := make(map[string]string)
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".tmpl") {
			fullPath := filepath.Join(promptsDir, file.Name())
			content, err := os.ReadFile(fullPath)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Could not read prompt file: %s", file.Name())})
				log.Errorf("Could not read prompt file %s: %v", file.Name(), err)
				return
			}
			prompts[file.Name()] = string(content)
		}
	}
	c.JSON(http.StatusOK, prompts)
}

// updatePromptsHandler handles the POST /api/prompts endpoint
func updatePromptsHandler(c *gin.Context) {
	var req struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	// Basic input validation: require a .tmpl extension and a simple basename
	// (no path separators, no absolute paths, no dot-sequences).
	base := filepath.Base(req.Filename)
	if req.Filename == "" || base != req.Filename || !strings.HasSuffix(base, ".tmpl") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename: must be a plain filename ending in .tmpl"})
		return
	}
	if containsDotDot(req.Filename) || strings.ContainsAny(req.Filename, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename: path traversal is not allowed"})
		return
	}

	// Verify the resolved path stays inside the prompts directory.
	promptsDir, err := filepath.Abs("prompts")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	promptPath := filepath.Join(promptsDir, base)
	if !strings.HasPrefix(promptPath, promptsDir+string(os.PathSeparator)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename: path traversal is not allowed"})
		return
	}

	// Validate template content
	_, err = template.New(req.Filename).Option("missingkey=error").Funcs(sprig.FuncMap()).Parse(req.Content)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid template content: %v", err)})
		return
	}

	// Write the updated prompt file
	err = os.WriteFile(promptPath, []byte(req.Content), 0644)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write prompt file"})
		log.Errorf("Failed to write prompt file %s: %v", req.Filename, err)
		return
	}

	// Reload templates to apply changes immediately
	if err := loadTemplates(); err != nil {
		log.Errorf("Failed to reload templates after update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Prompt saved but failed to reload templates"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Prompt saved successfully"})
}

// getAllTagsHandler handles the GET /api/tags endpoint
func (app *App) getAllTagsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	tags, err := app.Client.GetAllTags(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching tags: %v", err)})
		log.Errorf("Error fetching tags: %v", err)
		return
	}

	c.JSON(http.StatusOK, tags)
}

// getSettingsHandler handles the GET /api/settings endpoint
func (app *App) getSettingsHandler(c *gin.Context) {
	// Trigger a background refresh so the next request sees fresher data,
	// but serve the current cached snapshot immediately to avoid blocking.
	go refreshCustomFieldsCache(app.Client)

	settingsMutex.RLock()
	settingsCopy := settings
	settingsMutex.RUnlock()
	customFieldsCacheMu.RLock()
	defer customFieldsCacheMu.RUnlock()

	settingsCopy = sanitizeSettingsForResponse(settingsCopy)
	webhookConfigured := app.isWebhookConfigured(c.Request.Context(), paperlessWebhookProvider)

	// Create a response that includes both settings and custom fields
	response := gin.H{
		"settings":      settingsCopy,
		"custom_fields": customFieldsCache,
		"webhooks": gin.H{
			"paperless_configured": webhookConfigured,
		},
	}
	c.JSON(http.StatusOK, response)
}

// updateSettingsHandler handles the POST /api/settings endpoint
func (app *App) updateSettingsHandler(c *gin.Context) {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()

	var patch map[string]interface{}
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	merged, err := mergeSettingsPatch(settings, patch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := mergeSecretSettings(settings, &merged); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save encrypted integration secret"})
		return
	}
	webhookSecret := strings.TrimSpace(merged.PaperlessWebhookSecret)
	merged.PaperlessWebhookSecret = ""

	settings = merged

	// Save the updated settings to file
	if err := saveSettingsLocked(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		log.Errorf("Failed to save settings: %v", err)
		return
	}
	if webhookSecret != "" {
		if err := app.upsertWebhookSecret(c.Request.Context(), paperlessWebhookProvider, webhookSecret); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save Paperless webhook secret"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings saved successfully"})
}

func (app *App) getAIProviderSettingsHandler(c *gin.Context) {
	response, err := app.getAIProviderSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI provider settings"})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (app *App) updateAIProviderSettingsHandler(c *gin.Context) {
	var req AIProviderSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	response, err := app.saveAIProviderSettings(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (app *App) testAIProviderSettingsHandler(c *gin.Context) {
	var req AIProviderSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	if err := app.testAIProviderSettings(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "AI provider connection succeeded"})
}

// getCustomFieldsHandler handles the GET /api/custom_fields endpoint
func (app *App) getCustomFieldsHandler(c *gin.Context) {
	// Check for "force_pull" query parameter
	if forcePull := c.Query("force_pull"); forcePull == "true" {
		// Force a refresh of the custom fields cache
		go refreshCustomFieldsCache(app.Client)
	}

	customFieldsCacheMu.RLock()
	defer customFieldsCacheMu.RUnlock()

	c.JSON(http.StatusOK, customFieldsCache)
}

// documentsHandler handles the GET /api/documents endpoint
func (app *App) documentsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	documents, err := app.Client.GetDocumentsByTag(ctx, manualTag, 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error fetching documents: %v", err)})
		log.Errorf("Error fetching documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, documents)
}

// generateSuggestionsHandler handles the POST /api/generate-suggestions endpoint
func (app *App) generateSuggestionsHandler(c *gin.Context) {
	ctx := c.Request.Context()

	var suggestionRequest GenerateSuggestionsRequest
	if err := c.ShouldBindJSON(&suggestionRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid request payload: %v", err)
		return
	}

	results, err := app.generateDocumentSuggestionsCached(ctx, suggestionRequest, log.WithContext(ctx))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error processing documents: %v", err)})
		log.Errorf("Error processing documents: %v", err)
		return
	}

	c.JSON(http.StatusOK, results)
}

// updateDocumentsHandler handles the PATCH /api/update-documents endpoint
func (app *App) updateDocumentsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var documents []DocumentSuggestion
	if err := c.ShouldBindJSON(&documents); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request payload: %v", err)})
		log.Errorf("Invalid request payload: %v", err)
		return
	}

	batch, err := app.createApplyBatch(ctx, documents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create apply batch"})
		log.Errorf("Failed to create apply batch: %v", err)
		return
	}

	err = app.Client.UpdateDocuments(ctx, documents, app.Database, false, batch.ID)
	if err != nil {
		_ = app.finishApplyBatch(ctx, batch.ID, fmt.Sprintf("failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error updating documents: %v", err)})
		log.Errorf("Error updating documents: %v", err)
		return
	}

	settingsMutex.RLock()
	jobberEnabled := settings.JobberEnabled
	jobberExpenseEnabled := settings.JobberExpenseEnabled
	googleDriveFolderID := settings.GoogleDriveFolderID
	settingsMutex.RUnlock()

	results := make([]DocumentIntegrationResult, 0, len(documents))
	for _, document := range documents {
		result := DocumentIntegrationResult{
			DocumentID:       document.ID,
			PaperlessUpdated: true,
		}

		if selectedCandidate, ok := getSelectedJobberCandidate(document); ok {
			if !document.ApplyJobber {
				log.WithField("document_id", document.ID).Debug("Jobber job selected but per-document Jobber apply is disabled; skipping")
			} else if !jobberEnabled {
				log.WithField("document_id", document.ID).Debug("Jobber job selected but job matching is disabled in settings; skipping field write")
			} else {
				applied, err := app.applyJobberSelection(ctx, document.ID, selectedCandidate, &batch.ID)
				if err != nil {
					result.JobberError = err.Error()
					log.WithField("document_id", document.ID).WithError(err).Error("Failed to write Jobber fields to Paperless custom fields")
				} else {
					result.JobberApplied = applied
					if !applied {
						log.WithField("document_id", document.ID).Warn("Jobber job selected but no custom field mappings are configured — nothing was written to Paperless. Configure the field mappings under Settings → Integrations → Jobber.")
					}
				}
			}

			if document.ApplyJobber && document.CreateJobberExpense {
				if !jobberExpenseEnabled {
					log.WithField("document_id", document.ID).Debug("Jobber expense creation requested but expense creation is disabled in settings; skipping")
					result.JobberExpenseError = "expense creation is disabled in Settings → Integrations → Jobber"
				} else {
					expenseResult, err := app.Integrations.CreateJobberExpense(ctx, app.Client, document, selectedCandidate, batch.ID)
					if err != nil {
						result.JobberExpenseError = err.Error()
						log.WithField("document_id", document.ID).WithError(err).Error("Failed to create Jobber expense")
					} else {
						result.JobberExpenseCreated = true
						result.JobberExpenseID = expenseResult.ExpenseID
					}
				}
			}
		}

		if document.UploadToGoogleDrive {
			uploadResult, err := app.Integrations.UploadDocumentToGoogleDrive(ctx, app.Client, document.ID, googleDriveFolderID)
			if err != nil {
				result.GoogleDriveError = err.Error()
				log.WithField("document_id", document.ID).WithError(err).Error("Failed to upload document to Google Drive")
			} else {
				result.GoogleDriveUploaded = true
				result.GoogleDriveFileID = uploadResult.FileID
				result.GoogleDriveURL = uploadResult.FileURL
			}
		}
		if document.ApplyFirefly {
			fireflyResult, err := app.Integrations.ApplyFirefly(ctx, app.Client, document, batch.ID)
			if err != nil {
				result.FireflyError = err.Error()
				if fireflyResult != nil {
					result.FireflyMatched = fireflyResult.Matched
					result.FireflyCreated = fireflyResult.Created
					result.FireflyTransactionID = fireflyResult.TransactionID
					result.FireflyURL = fireflyResult.URL
				}
			} else if fireflyResult != nil {
				result.FireflyMatched = fireflyResult.Matched
				result.FireflyCreated = fireflyResult.Created
				result.FireflyAttachmentUploaded = fireflyResult.AttachmentUploaded
				result.FireflyTransactionID = fireflyResult.TransactionID
				result.FireflyURL = fireflyResult.URL
			}
		}
		if document.UploadToQuickBooks {
			qboResult, err := app.Integrations.UploadQuickBooksReceipt(ctx, app.Client, document, batch.ID)
			if err != nil {
				result.QuickBooksError = err.Error()
			} else {
				result.QuickBooksUploaded = true
				result.QuickBooksAttachableID = qboResult.AttachableID
				result.QuickBooksURL = qboResult.URL
			}
		}

		results = append(results, result)
	}

	_ = app.finishApplyBatch(ctx, batch.ID, fmt.Sprintf("Applied %d document(s)", len(documents)))

	c.JSON(http.StatusOK, gin.H{"results": results, "batch_id": batch.ID})
}

func (app *App) createApplyBatch(ctx context.Context, documents []DocumentSuggestion) (*ApplyBatch, error) {
	return CreateApplyBatch(app.Database.WithContext(ctx), len(documents), fmt.Sprintf("Applying %d document(s)", len(documents)))
}

func (app *App) finishApplyBatch(ctx context.Context, batchID uint, summary string) error {
	return FinishApplyBatch(app.Database.WithContext(ctx), batchID, summary)
}

func (app *App) getIntegrationsStatusHandler(c *gin.Context) {
	statuses := []IntegrationConnectionStatus{
		app.Integrations.Status("jobber"),
		app.Integrations.Status("google_drive"),
		app.Integrations.Status("quickbooks"),
		app.Integrations.FireflyStatus(c.Request.Context()),
	}

	c.JSON(http.StatusOK, gin.H{"providers": statuses})
}

func (app *App) getIntegrationStatusHandler(c *gin.Context) {
	provider := c.Param("provider")
	status := app.Integrations.Status(provider)
	if status.Provider == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Unknown provider"})
		return
	}

	c.JSON(http.StatusOK, status)
}

// getIntegrationActionLogHandler returns paginated integration action log entries.
// Query params: page (default 1), pageSize (default 20, max 100), provider (optional filter).
func (app *App) getIntegrationActionLogHandler(c *gin.Context) {
	page := 1
	pageSize := 20
	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(c.DefaultQuery("pageSize", "20")); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}
	provider := strings.TrimSpace(c.Query("provider"))

	db := app.Database.WithContext(c.Request.Context()).Model(&IntegrationActionLog{})
	if provider != "" {
		db = db.Where("provider = ?", provider)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count action log entries"})
		return
	}

	var entries []IntegrationActionLog
	offset := (page - 1) * pageSize
	if err := db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&entries).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve action log entries"})
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize
	c.JSON(http.StatusOK, gin.H{
		"items":       entries,
		"totalItems":  total,
		"totalPages":  totalPages,
		"currentPage": page,
		"pageSize":    pageSize,
	})
}

func (app *App) startIntegrationConnectHandler(c *gin.Context) {
	provider := c.Param("provider")

	impl := getIntegrationProvider(provider)
	if impl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown integration provider"})
		return
	}
	configured, reason := impl.Configured()
	if !configured {
		c.JSON(http.StatusBadRequest, gin.H{"error": reason})
		return
	}

	state, err := generateOAuthStateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state token"})
		return
	}
	if err := saveOAuthState(app.Database.WithContext(c.Request.Context()), provider, state, "/settings"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save oauth state"})
		return
	}

	authURL, err := impl.AuthorizationURL(c, state)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"redirect_url": authURL})
}

// oauthPopupHTMLTemplate is rendered for OAuth callback popup windows.
// Dynamic values are placed only inside JSON (safe for script) or as escaped
// HTML text — never as raw HTML or unescaped script strings.
const oauthPopupHTMLTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>%s</title></head>
<body>
<script>
(function() {
  var msg = %s;
  if (window.opener) {
    try { window.opener.postMessage(msg, window.location.origin); } catch(e) {}
  }
  setTimeout(function() { window.close(); }, 2000);
})();
</script>
<p>%s</p>
</body>
</html>`

func oauthPopupResponse(c *gin.Context, status int, title, message string, msgPayload interface{}) {
	jsonBytes, err := json.Marshal(msgPayload)
	if err != nil {
		log.Errorf("oauthPopupResponse: failed to marshal payload: %v", err)
		jsonBytes = []byte("{}")
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	// HTML-escape all user-visible strings so query-parameter values cannot
	// inject HTML or script into the popup page.
	c.String(status, oauthPopupHTMLTemplate,
		htmlEscape(title),
		string(jsonBytes), // already safe JSON
		htmlEscape(message),
	)
}

// htmlEscape escapes s for safe inclusion in HTML text content.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&#34;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func (app *App) integrationOAuthCallbackHandler(c *gin.Context) {
	provider := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	// Handle provider-level OAuth errors (e.g. user denied access)
	if oauthErr := c.Query("error"); oauthErr != "" {
		errDesc := c.Query("error_description")
		if errDesc == "" {
			errDesc = oauthErr
		}
		log.Warnf("OAuth error from provider %s: %s - %s", provider, oauthErr, errDesc)
		oauthPopupResponse(c, http.StatusBadRequest,
			"Connection failed",
			"Integration connection failed: "+errDesc,
			gin.H{"type": "oauth_error", "error": errDesc},
		)
		return
	}

	stateRecord, err := consumeOAuthState(app.Database.WithContext(c.Request.Context()), provider, state)
	if err != nil {
		oauthPopupResponse(c, http.StatusBadRequest,
			"Connection failed",
			"Integration connection failed: invalid or expired state. Please try connecting again.",
			gin.H{"type": "oauth_error", "error": "invalid or expired state"},
		)
		return
	}

	impl := getIntegrationProvider(provider)
	if impl == nil {
		oauthPopupResponse(c, http.StatusBadRequest,
			"Connection failed",
			"Integration connection failed: unknown provider.",
			gin.H{"type": "oauth_error", "error": "unknown provider"},
		)
		return
	}

	token, err := impl.ExchangeCode(c.Request.Context(), c, code)
	if err != nil {
		log.WithError(err).Errorf("OAuth code exchange failed for provider %s", provider)
		oauthPopupResponse(c, http.StatusBadRequest,
			"Connection failed",
			"Integration connection failed: could not exchange authorization code. Please verify your redirect URL and app credentials.",
			gin.H{"type": "oauth_error", "error": "code exchange failed"},
		)
		return
	}

	identity, err := impl.FetchIdentity(c.Request.Context(), &IntegrationConnection{
		Provider:             provider,
		AccessToken:          token.AccessToken,
		RefreshToken:         token.RefreshToken,
		AccessTokenExpiresAt: token.ExpiresAt,
	})
	if err != nil {
		log.WithError(err).Warnf("failed to fetch identity for provider %s after OAuth", provider)
	}

	if _, err := upsertIntegrationConnection(app.Database.WithContext(c.Request.Context()), provider, token, identity); err != nil {
		oauthPopupResponse(c, http.StatusInternalServerError,
			"Connection failed",
			"Integration connection failed: could not save connection.",
			gin.H{"type": "oauth_error", "error": "could not persist connection"},
		)
		return
	}

	_ = stateRecord

	oauthPopupResponse(c, http.StatusOK,
		"Connected",
		"Integration connected successfully. You can close this window.",
		gin.H{"type": "oauth_success"},
	)
}

func (app *App) disconnectIntegrationHandler(c *gin.Context) {
	provider := c.Param("provider")
	if err := app.Integrations.Disconnect(c.Request.Context(), provider); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Disconnected"})
}

func (app *App) jobberReceiptHandler(c *gin.Context) {
	token := c.Param("token")
	if strings.TrimSpace(token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing receipt token"})
		return
	}

	share, err := app.Integrations.ConsumeReceiptAccessToken(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Receipt token not found or expired"})
		return
	}

	document, err := app.Client.GetDocument(c.Request.Context(), share.DocumentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load document: %v", err)})
		return
	}

	content, err := app.Client.DownloadPDF(c.Request.Context(), document)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to download document: %v", err)})
		return
	}

	filename := strings.TrimSpace(document.ArchivedFileName)
	if filename == "" {
		filename = strings.TrimSpace(document.OriginalFileName)
	}
	if filename == "" {
		filename = fmt.Sprintf("document-%d.pdf", share.DocumentID)
	}

	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", safeContentDisposition("inline", filename))
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(content)
}

func (app *App) jobberMatchCandidatesHandler(c *gin.Context) {
	var req struct {
		// DocumentIDs lists the documents to rank candidates for.
		DocumentIDs []int `json:"document_ids"`
		// Documents, if provided, supplies the full document objects so the
		// handler does not need to fetch them from Paperless.  Elements are
		// matched by position/ID to DocumentIDs.
		Documents []Document `json:"documents,omitempty"`
		// SuggestedCreatedDates maps document ID -> normalized YYYY-MM-DD date
		// that the LLM just suggested. When present, this date is used for
		// matching instead of the document's CreatedDate from Paperless: the
		// whole point of running the LLM is that Paperless's date is often
		// wrong, so matching against it would be too.
		SuggestedCreatedDates map[string]string `json:"suggested_created_dates,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		log.WithError(err).Warn("jobberMatchCandidatesHandler: invalid request payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	if len(req.DocumentIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"candidates": map[int][]JobberMatchCandidate{}})
		return
	}

	ctx := c.Request.Context()

	// Build a lookup map from the documents provided inline (avoids extra Paperless API calls).
	docByID := make(map[int]Document, len(req.Documents))
	for _, d := range req.Documents {
		docByID[d.ID] = d
	}

	// For any document ID not supplied inline, fetch from Paperless in parallel.
	missing := make([]int, 0)
	for _, id := range req.DocumentIDs {
		if _, ok := docByID[id]; !ok {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		type fetchResult struct {
			doc Document
			err error
		}
		ch := make(chan fetchResult, len(missing))
		eg, egCtx := errgroup.WithContext(ctx)
		for _, documentID := range missing {
			documentID := documentID
			eg.Go(func() error {
				doc, err := app.Client.GetDocument(egCtx, documentID)
				ch <- fetchResult{doc: doc, err: err}
				return nil // collect errors via channel; don't abort siblings
			})
		}
		_ = eg.Wait()
		close(ch)
		for r := range ch {
			if r.err != nil {
				log.WithError(r.err).Warnf("jobberMatchCandidatesHandler: could not fetch document %d from Paperless; skipping ranking for this document", r.doc.ID)
				continue
			}
			docByID[r.doc.ID] = r.doc
		}
	}

	// Serve cached ranked lists first. Any misses share one Jobber API fetch.
	results := make(map[int][]JobberMatchCandidate, len(req.DocumentIDs))
	autoSelected := make(map[int]string, len(req.DocumentIDs))
	misses := make([]int, 0)
	for _, id := range req.DocumentIDs {
		doc, ok := docByID[id]
		if !ok {
			continue
		}
		preferred := req.SuggestedCreatedDates[strconv.Itoa(id)]
		matchHash := jobberCandidateMatchHash(doc, preferred)
		var cached []JobberMatchCandidate
		cachedAuto, hit, err := app.getCachedIntegrationCandidates(ctx, id, integrationCandidateProviderJobber, matchHash, &cached)
		if err != nil {
			log.WithError(err).WithField("document_id", id).Warn("Failed to read cached Jobber candidates; refetching")
		}
		if hit {
			results[id] = cached
			if cachedAuto != "" {
				autoSelected[id] = cachedAuto
			}
			continue
		}
		misses = append(misses, id)
	}

	var allCandidates []JobberMatchCandidate
	if len(misses) > 0 {
		var err error
		// Fetch the full Jobber job list once — all documents share the same universe
		// of candidates, so making one round of paginated API calls is enough.
		allCandidates, err = app.Integrations.FetchAllJobberCandidates(ctx)
		if err != nil {
			log.WithError(err).
				WithField("document_count", len(req.DocumentIDs)).
				Error("jobberMatchCandidatesHandler: failed to fetch Jobber candidates")
			status := http.StatusInternalServerError
			errorCode := "jobber_fetch_failed"
			if errors.Is(err, errJobberNotConnected) {
				status = http.StatusBadGateway
				errorCode = "jobber_not_connected"
			} else if errors.Is(err, errJobberAuthFailed) {
				status = http.StatusBadGateway
				errorCode = "jobber_auth_failed"
			}
			c.JSON(status, gin.H{
				"error": fmt.Sprintf("error fetching Jobber jobs: %v", err),
				"code":  errorCode,
			})
			return
		}
	}

	for _, id := range misses {
		doc, ok := docByID[id]
		if !ok {
			results[id] = allCandidates
			continue
		}
		preferred := req.SuggestedCreatedDates[strconv.Itoa(id)]
		ranked, _ := rankJobberCandidatesWithSelection(doc, preferred, allCandidates)
		results[id] = ranked.Candidates
		if ranked.AutoSelectedID != "" {
			autoSelected[id] = ranked.AutoSelectedID
		}
		matchHash := jobberCandidateMatchHash(doc, preferred)
		if err := app.saveIntegrationCandidates(ctx, id, integrationCandidateProviderJobber, matchHash, ranked.Candidates, ranked.AutoSelectedID); err != nil {
			log.WithError(err).WithField("document_id", id).Warn("Failed to cache Jobber candidates")
		}
	}

	c.JSON(http.StatusOK, gin.H{"candidates": results, "auto_selected": autoSelected})
}

func (app *App) fireflyMatchCandidatesHandler(c *gin.Context) {
	var req struct {
		DocumentIDs           []int                `json:"document_ids"`
		Documents             []Document           `json:"documents,omitempty"`
		Suggestions           []DocumentSuggestion `json:"suggestions,omitempty"`
		SuggestedCreatedDates map[string]string    `json:"suggested_created_dates,omitempty"`
		Amounts               map[string]string    `json:"amounts,omitempty"`
		Currencies            map[string]string    `json:"currencies,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	ctx := c.Request.Context()
	docByID := make(map[int]Document, len(req.Documents))
	for _, doc := range req.Documents {
		docByID[doc.ID] = doc
	}
	suggestionByID := make(map[int]DocumentSuggestion, len(req.Suggestions))
	for _, suggestion := range req.Suggestions {
		suggestionByID[suggestion.ID] = suggestion
	}
	results := make(map[int][]FireflyTransactionCandidate, len(req.DocumentIDs))
	autoSelected := make(map[int]string, len(req.DocumentIDs))
	for _, id := range req.DocumentIDs {
		suggestion, ok := suggestionByID[id]
		if !ok {
			doc := docByID[id]
			if doc.ID == 0 {
				fetched, err := app.Client.GetDocument(ctx, id)
				if err != nil {
					log.WithError(err).WithField("document_id", id).Warn("Could not fetch document for Firefly matching")
					continue
				}
				doc = fetched
			}
			suggestion = DocumentSuggestion{ID: id, OriginalDocument: doc}
		}
		if date := strings.TrimSpace(req.SuggestedCreatedDates[strconv.Itoa(id)]); date != "" {
			suggestion.SuggestedCreatedDate = date
		}
		if amount := strings.TrimSpace(req.Amounts[strconv.Itoa(id)]); amount != "" {
			suggestion.SuggestedCustomFields = append(suggestion.SuggestedCustomFields, CustomFieldSuggestion{Name: "amount", Value: amount})
		}
		if currency := strings.TrimSpace(req.Currencies[strconv.Itoa(id)]); currency != "" {
			suggestion.SuggestedCustomFields = append(suggestion.SuggestedCustomFields, CustomFieldSuggestion{Name: "currency", Value: currency})
		}
		matchHash := fireflyCandidateMatchHash(suggestion)
		var cached []FireflyTransactionCandidate
		cachedAuto, hit, err := app.getCachedIntegrationCandidates(ctx, id, integrationCandidateProviderFirefly, matchHash, &cached)
		if err == nil && hit {
			results[id] = cached
			if cachedAuto != "" {
				autoSelected[id] = cachedAuto
			}
			continue
		}
		candidates, auto, err := app.Integrations.FetchFireflyTransactionCandidates(ctx, suggestion)
		if err != nil {
			results[id] = []FireflyTransactionCandidate{}
			log.WithError(err).WithField("document_id", id).Warn("Firefly candidates unavailable")
			continue
		}
		results[id] = candidates
		if auto != "" {
			autoSelected[id] = auto
		}
		if err := app.saveIntegrationCandidates(ctx, id, integrationCandidateProviderFirefly, matchHash, candidates, auto); err != nil {
			log.WithError(err).WithField("document_id", id).Warn("Failed to cache Firefly candidates")
		}
	}
	c.JSON(http.StatusOK, gin.H{"candidates": results, "auto_selected": autoSelected})
}

func jobberCandidateMatchHash(doc Document, preferredDate string) string {
	return integrationMatchHash(integrationCandidateProviderJobber, map[string]interface{}{
		"document_created_date":  doc.CreatedDate,
		"suggested_created_date": strings.TrimSpace(preferredDate),
	})
}

func fireflyCandidateMatchHash(suggestion DocumentSuggestion) string {
	cfg, _, _ := fireflyConfigFromSettings()
	derived, _ := deriveFireflyTransaction(suggestion, cfg)
	return integrationMatchHash(integrationCandidateProviderFirefly, map[string]interface{}{
		"document_id": suggestion.ID,
		"date":        derived.Date,
		"amount":      derived.AmountString,
		"currency":    derived.CurrencyCode,
		"instance":    cfg.InstanceURL,
		"source":      cfg.DefaultSourceAccount,
		"destination": cfg.DefaultDestinationAccount,
	})
}

func getSelectedFireflyCandidate(document DocumentSuggestion) (FireflyTransactionCandidate, bool) {
	if document.SelectedFireflyTransactionID == "" {
		return FireflyTransactionCandidate{}, false
	}
	for _, candidate := range document.FireflyCandidates {
		if candidate.ID == document.SelectedFireflyTransactionID {
			return candidate, true
		}
	}
	return FireflyTransactionCandidate{ID: document.SelectedFireflyTransactionID}, true
}

func currentBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = forwardedProto
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func getSelectedJobberCandidate(document DocumentSuggestion) (JobberMatchCandidate, bool) {
	if document.SelectedJobberMatchID == "" {
		return JobberMatchCandidate{}, false
	}

	for _, candidate := range document.JobberCandidates {
		if candidate.ID == document.SelectedJobberMatchID {
			return candidate, true
		}
	}

	return JobberMatchCandidate{}, false
}

// applyJobberSelection writes the selected Jobber job's details into the
// Paperless-ngx custom fields that are configured under Settings → Integrations.
// It returns (true, nil) when at least one field was written, (false, nil) when
// no field mappings are configured (a no-op, not an error), and (false, err)
// when the Paperless API call failed.
func (app *App) applyJobberSelection(ctx context.Context, documentID int, candidate JobberMatchCandidate, batchID *uint) (bool, error) {
	settingsMutex.RLock()
	jobIDFieldID := settings.JobberJobIDFieldID
	jobNumberFieldID := settings.JobberJobNumberFieldID
	clientFieldID := settings.JobberClientFieldID
	jobNameFieldID := settings.JobberJobNameFieldID
	settingsMutex.RUnlock()

	fieldValues := map[int]interface{}{}
	if jobIDFieldID > 0 {
		fieldValues[jobIDFieldID] = candidate.ID
	}
	if jobNumberFieldID > 0 {
		fieldValues[jobNumberFieldID] = candidate.JobNumber
	}
	if clientFieldID > 0 {
		fieldValues[clientFieldID] = candidate.ClientName
	}
	if jobNameFieldID > 0 {
		fieldValues[jobNameFieldID] = candidate.JobName
	}

	if len(fieldValues) == 0 {
		log.WithField("document_id", documentID).
			WithFields(map[string]interface{}{
				"configured_job_id_field":     jobIDFieldID,
				"configured_job_number_field": jobNumberFieldID,
				"configured_client_field":     clientFieldID,
				"configured_job_name_field":   jobNameFieldID,
			}).
			Debug("No Jobber → Paperless field mappings configured; skipping custom field update")
		return false, nil
	}

	log.WithField("document_id", documentID).
		WithField("jobber_job_id", candidate.ID).
		WithField("jobber_job_number", candidate.JobNumber).
		WithField("fields_to_write", len(fieldValues)).
		Info("Writing Jobber job details to Paperless custom fields")

	if err := app.Client.UpsertDocumentCustomFieldsWithBatch(ctx, documentID, fieldValues, app.Database, batchID); err != nil {
		return false, err
	}
	return true, nil
}

func mergeSettingsPatch(current Settings, patch map[string]interface{}) (Settings, error) {
	rawCurrent, err := json.Marshal(current)
	if err != nil {
		return Settings{}, err
	}

	currentMap := map[string]interface{}{}
	if err := json.Unmarshal(rawCurrent, &currentMap); err != nil {
		return Settings{}, err
	}

	for key, value := range patch {
		currentMap[key] = value
	}

	rawMerged, err := json.Marshal(currentMap)
	if err != nil {
		return Settings{}, err
	}

	var merged Settings
	if err := json.Unmarshal(rawMerged, &merged); err != nil {
		return Settings{}, fmt.Errorf("invalid settings payload: %w", err)
	}

	if merged.CustomFieldsWriteMode == "" {
		merged.CustomFieldsWriteMode = current.CustomFieldsWriteMode
	}

	return merged, nil
}

func (app *App) submitOCRJobHandler(c *gin.Context) {
	if !app.isOcrEnabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "OCR is not enabled on this server"})
		return
	}

	documentIDStr := c.Param("id")
	documentID, err := strconv.Atoi(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	// Create a new job
	jobID := generateJobID() // Implement a function to generate unique job IDs
	job := &Job{
		ID:         jobID,
		DocumentID: documentID,
		Status:     "pending",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Add job to store and queue
	jobStore.addJob(job)
	jobQueue <- job

	// Return the job ID to the client
	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

func (app *App) getJobStatusHandler(c *gin.Context) {
	jobID := c.Param("job_id")

	job, exists := jobStore.getJob(jobID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	response := gin.H{
		"job_id":      job.ID,
		"status":      job.Status,
		"created_at":  job.CreatedAt,
		"updated_at":  job.UpdatedAt,
		"pages_done":  job.PagesDone,
		"total_pages": job.TotalPages,
	}

	if job.Status == "completed" {
		response["result"] = job.Result
	} else if job.Status == "failed" {
		response["error"] = job.Result
	}

	c.JSON(http.StatusOK, response)
}

func (app *App) getAllJobsHandler(c *gin.Context) {
	jobs := jobStore.GetAllJobs()

	jobList := make([]gin.H, 0, len(jobs))
	for _, job := range jobs {
		response := gin.H{
			"job_id":      job.ID,
			"status":      job.Status,
			"created_at":  job.CreatedAt,
			"updated_at":  job.UpdatedAt,
			"pages_done":  job.PagesDone,
			"total_pages": job.TotalPages,
		}

		if job.Status == "completed" {
			response["result"] = job.Result
		} else if job.Status == "failed" {
			response["error"] = job.Result
		}

		jobList = append(jobList, response)
	}

	c.JSON(http.StatusOK, jobList)
}

// POST /api/ocr/jobs/:job_id/stop
func (app *App) stopOCRJobHandler(c *gin.Context) {
	jobID := c.Param("job_id")
	jobCancellersMu.Lock()
	cancel, exists := jobCancellers[jobID]
	jobCancellersMu.Unlock()
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No running job with this ID"})
		return
	}
	cancel()
	c.Status(http.StatusNoContent)
}

// deleteDocumentHandler handles DELETE /api/documents/:id — permanently removes a document from Paperless-ngx
func (app *App) deleteDocumentHandler(c *gin.Context) {
	id := c.Param("id")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	if err := app.Client.DeleteDocument(c.Request.Context(), parsedID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error deleting document: %v", err)})
		log.Errorf("Error deleting document %d: %v", parsedID, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// getDocumentHandler handles the retrieval of a document by its ID
func (app *App) getDocumentHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		parsedID, err := strconv.Atoi(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
			return
		}
		document, err := app.Client.GetDocument(c, parsedID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			log.Errorf("Error fetching document: %v", err)
			return
		}
		c.JSON(http.StatusOK, document)
	}
}

// getOCRPagesHandler returns per-page OCR results for a document
func (app *App) getOCRPagesHandler(c *gin.Context) {
	id := c.Param("id")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	dbResults, err := GetOcrPageResults(app.Database, parsedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch OCR page results"})
		return
	}

	type OCRPageResult struct {
		Text           string                 `json:"text"`
		OcrLimitHit    bool                   `json:"ocrLimitHit"`
		GenerationInfo map[string]interface{} `json:"generationInfo,omitempty"`
	}

	var pages []OCRPageResult
	for _, res := range dbResults {
		var genInfo map[string]interface{}
		if res.GenerationInfo != "" {
			_ = json.Unmarshal([]byte(res.GenerationInfo), &genInfo)
		}
		pages = append(pages, OCRPageResult{
			Text:           res.Text,
			OcrLimitHit:    res.OcrLimitHit,
			GenerationInfo: genInfo,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"pages": pages,
	})
}

func (app *App) reOCRPageHandler(c *gin.Context) {
	id := c.Param("id")
	pageIdxStr := c.Param("pageIndex")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	pageIdx, err := strconv.Atoi(pageIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	// Validate page index before downloading
	if pageIdx < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	// Only download up to the needed page (pageIdx is 0-based, so limit = pageIdx+1).
	imagePaths, _, err := app.Client.DownloadDocumentAsImages(c.Request.Context(), parsedID, pageIdx+1)
	if err != nil || pageIdx >= len(imagePaths) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index or failed to download images"})
		return
	}
	imageContent, err := os.ReadFile(imagePaths[pageIdx])
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read image file"})
		return
	}

	cancelKey := fmt.Sprintf("%d-%d", parsedID, pageIdx)
	reOcrCtx, cancelReOcr := context.WithCancel(c.Request.Context())
	defer cancelReOcr()

	reOcrCancellersMu.Lock()
	if existingCancel, ok := reOcrCancellers[cancelKey]; ok {
		existingCancel()
	}
	reOcrCancellers[cancelKey] = cancelReOcr
	reOcrCancellersMu.Unlock()

	defer func() {
		reOcrCancellersMu.Lock()
		delete(reOcrCancellers, cancelKey)
		reOcrCancellersMu.Unlock()
	}()

	result, err := app.ocrProvider.ProcessImage(reOcrCtx, imageContent, pageIdx+1)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Infof("Re-OCR for doc %d page %d cancelled.", parsedID, pageIdx)
			c.Status(499)
		} else {
			log.Errorf("Failed to re-OCR doc %d page %d: %v", parsedID, pageIdx, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to re-OCR page"})
		}
		return
	}
	if result == nil {
		log.Errorf("Re-OCR for doc %d page %d returned nil result.", parsedID, pageIdx)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Re-OCR returned no result"})
		return
	}

	var genInfoJSON string
	if result.GenerationInfo != nil {
		if b, err := json.Marshal(result.GenerationInfo); err == nil {
			genInfoJSON = string(b)
		}
	}
	saveErr := SaveSingleOcrPageResult(app.Database, parsedID, pageIdx, result.Text, result.OcrLimitHit, genInfoJSON)
	if saveErr != nil {
		log.Errorf("Failed to save re-OCR result for doc %d page %d: %v", parsedID, pageIdx, saveErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Re-OCR succeeded but failed to persist result"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"text":           result.Text,
		"ocrLimitHit":    result.OcrLimitHit,
		"generationInfo": result.GenerationInfo,
	})
}

// cancelReOCRPageHandler handles the DELETE request to cancel an ongoing re-OCR for a specific page.
func (app *App) cancelReOCRPageHandler(c *gin.Context) {
	id := c.Param("id")
	pageIdxStr := c.Param("pageIndex")
	parsedID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	pageIdx, err := strconv.Atoi(pageIdxStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page index"})
		return
	}

	cancelKey := fmt.Sprintf("%d-%d", parsedID, pageIdx)

	reOcrCancellersMu.Lock()
	cancel, exists := reOcrCancellers[cancelKey]
	if exists {
		delete(reOcrCancellers, cancelKey)
	}
	reOcrCancellersMu.Unlock()

	if exists {
		cancel()
		log.Infof("Cancellation requested for re-OCR doc %d page %d", parsedID, pageIdx)
		c.Status(http.StatusNoContent)
	} else {
		log.Warnf("No active re-OCR found to cancel for doc %d page %d", parsedID, pageIdx)
		c.JSON(http.StatusNotFound, gin.H{"error": "No active re-OCR operation found for this page"})
	}
}

// Section for local-db actions

func (app *App) getModificationHistoryHandler(c *gin.Context) {
	// Parse pagination parameters
	page := 1
	pageSize := 20

	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(c.DefaultQuery("pageSize", "20")); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	// Get paginated modifications and total count
	modifications, total, err := GetPaginatedModifications(app.Database, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve modification history"})
		log.Errorf("Failed to retrieve modification history: %v", err)
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"items":       modifications,
		"totalItems":  total,
		"totalPages":  totalPages,
		"currentPage": page,
		"pageSize":    pageSize,
	})
}

func (app *App) getApplyBatchHistoryHandler(c *gin.Context) {
	page := 1
	pageSize := 20
	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(c.DefaultQuery("pageSize", "20")); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	items, total, err := GetPaginatedApplyBatches(app.Database.WithContext(c.Request.Context()), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve apply batch history"})
		return
	}
	totalPages := (int(total) + pageSize - 1) / pageSize
	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"totalItems":  total,
		"totalPages":  totalPages,
		"currentPage": page,
		"pageSize":    pageSize,
	})
}

func (app *App) undoModificationHandler(c *gin.Context) {
	id := c.Param("id")
	modID, err := strconv.Atoi(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid modification ID"})
		log.Errorf("Invalid modification ID: %v", err)
		return
	}

	modification, err := GetModification(app.Database, uint(modID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve modification"})
		log.Errorf("Failed to retrieve modification: %v", err)
		return
	}

	if modification.Undone {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Modification has already been undone"})
		log.Errorf("Modification has already been undone: %v", id)
		return
	}

	if err := app.undoSingleModification(c.Request.Context(), modification); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update document"})
		log.Errorf("Failed to undo modification: %v", err)
		return
	}

	c.Status(http.StatusOK)
}

func (app *App) undoApplyBatchHandler(c *gin.Context) {
	id := c.Param("id")
	batchID, err := strconv.Atoi(id)
	if err != nil || batchID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch ID"})
		return
	}

	ctx := c.Request.Context()
	batch, err := GetApplyBatch(app.Database.WithContext(ctx), uint(batchID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Apply batch not found"})
		return
	}
	if batch.Undone {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Apply batch has already been undone"})
		return
	}

	var modifications []ModificationHistory
	if err := app.Database.WithContext(ctx).
		Where("batch_id = ? AND undone = ?", batch.ID, false).
		Order("id DESC").
		Find(&modifications).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve batch modifications"})
		return
	}

	for i := range modifications {
		if err := app.undoSingleModification(ctx, &modifications[i]); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to undo modification %d: %v", modifications[i].ID, err)})
			return
		}
	}

	if err := SetApplyBatchUndone(app.Database.WithContext(ctx), batch); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to mark batch as undone"})
		return
	}
	c.Status(http.StatusOK)
}

func (app *App) undoSingleModification(ctx context.Context, modification *ModificationHistory) error {
	if modification.Undone {
		return nil
	}

	var suggestion DocumentSuggestion
	suggestion.ID = int(modification.DocumentID)
	var err error
	suggestion.OriginalDocument, err = app.Client.GetDocument(ctx, int(modification.DocumentID))
	if err != nil {
		return fmt.Errorf("failed to retrieve current document: %w", err)
	}
	switch modification.ModField {
	case "title":
		suggestion.SuggestedTitle = modification.PreviousValue
	case "tags":
		var tags []string
		if err := json.Unmarshal([]byte(modification.PreviousValue), &tags); err != nil {
			return fmt.Errorf("failed to unmarshal previous tags: %w", err)
		}
		suggestion.SuggestedTags = tags
	case "content":
		suggestion.SuggestedContent = modification.PreviousValue
	case "correspondent":
		suggestion.SuggestedCorrespondent = modification.PreviousValue
	case "document_type":
		suggestion.SuggestedDocumentType = modification.PreviousValue
	case "created_date":
		suggestion.SuggestedCreatedDate = modification.PreviousValue
	default:
		return fmt.Errorf("invalid modification field: %s", modification.ModField)
	}

	if err := app.Client.UpdateDocuments(ctx, []DocumentSuggestion{suggestion}, app.Database, true); err != nil {
		return err
	}
	return SetModificationUndone(app.Database, modification)
}

func (app *App) reprocessDocumentHandler(c *gin.Context) {
	id := c.Param("id")
	documentID, err := strconv.Atoi(id)
	if err != nil || documentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	ctx := c.Request.Context()
	document, err := app.Client.GetDocument(ctx, documentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load document: %v", err)})
		return
	}

	if err := app.invalidateDocumentSuggestionCache(ctx, documentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to invalidate cached suggestions"})
		return
	}

	suggestion := DocumentSuggestion{
		ID:               documentID,
		OriginalDocument: document,
		SuggestedTags:    []string{manualTag},
		KeepOriginalTags: true,
	}
	if err := app.Client.UpdateDocuments(ctx, []DocumentSuggestion{suggestion}, app.Database, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to re-apply filter tag: %v", err)})
		return
	}
	if err := app.enqueueSuggestionJob(ctx, documentID, ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enqueue document for preprocessing"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"document_id": documentID, "status": "queued"})
}

// getVersionHandler handles the GET /api/version endpoint
func getVersionHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":   version,
		"commit":    commit,
		"buildDate": buildDate,
	})
}

// containsDotDot checks if a string contains ".." to prevent path traversal.
func containsDotDot(s string) bool {
	return strings.Contains(s, "..")
}

// safeContentDisposition builds a Content-Disposition header value that is safe
// against header injection by stripping CR/LF and quoting control characters.
// It follows RFC 6266 by providing both a sanitised ASCII fallback and a
// percent-encoded UTF-8 filename* parameter.
func safeContentDisposition(disposition, filename string) string {
	// Strip characters that would break the header (CR, LF, NUL, double-quote).
	safe := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 || r == '"' {
			return -1
		}
		return r
	}, filepath.Base(filename))
	if safe == "" || safe == "." {
		safe = "download"
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`,
		disposition, safe, url.PathEscape(filename))
}
