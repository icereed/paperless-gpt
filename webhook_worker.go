package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	paperlessWebhookProvider     = "paperless"
	paperlessSignatureHeader     = "X-Paperless-Signature"
	paperlessStaticSecretHeader  = "X-Paperless-GPT-Secret"
	suggestionJobStaleAfter      = 15 * time.Minute
	suggestionJobMaxRetryBackoff = 30 * time.Minute
)

type paperlessWebhookPayload struct {
	Event      string `json:"event"`
	DocumentID int    `json:"document_id"`
	Document   struct {
		ID int `json:"id"`
	} `json:"document"`
}

func (app *App) paperlessWebhookHandler(c *gin.Context) {
	ctx := c.Request.Context()
	secret, err := app.getWebhookSecret(ctx, paperlessWebhookProvider)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Paperless webhook secret is not configured"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read webhook body"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	if !validWebhookAuthentication(c, body, secret) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid webhook signature"})
		return
	}

	var payload paperlessWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook payload"})
		return
	}
	if payload.Event != "document.created" && payload.Event != "document.updated" {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored"})
		return
	}

	documentID := payload.DocumentID
	if documentID == 0 {
		documentID = payload.Document.ID
	}
	if documentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing document ID"})
		return
	}

	if payload.Event == "document.updated" {
		_ = app.invalidateDocumentSuggestionCache(ctx, documentID)
	}

	document, err := app.Client.GetDocument(ctx, documentID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Failed to load Paperless document: %v", err)})
		return
	}
	if !documentHasTag(document, manualTag) && !documentHasTag(document, autoTag) {
		c.JSON(http.StatusAccepted, gin.H{"status": "ignored", "document_id": documentID})
		return
	}

	if err := app.enqueueSuggestionJob(ctx, documentID, ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enqueue suggestion job"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "queued", "document_id": documentID})
}

func validWebhookAuthentication(c *gin.Context, body []byte, secret string) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}
	if provided := strings.TrimSpace(c.GetHeader(paperlessStaticSecretHeader)); provided != "" {
		return hmac.Equal([]byte(provided), []byte(secret))
	}
	return validWebhookSignature(c.GetHeader(paperlessSignatureHeader), body, secret)
}

func validWebhookSignature(header string, body []byte, secret string) bool {
	header = strings.TrimSpace(header)
	if header == "" || strings.TrimSpace(secret) == "" {
		return false
	}
	if strings.HasPrefix(header, "sha256=") {
		header = strings.TrimPrefix(header, "sha256=")
	}
	expectedMAC := hmac.New(sha256.New, []byte(secret))
	expectedMAC.Write(body)
	expected := hex.EncodeToString(expectedMAC.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(header)), []byte(expected))
}

func (app *App) getWebhookSecret(ctx context.Context, provider string) (string, error) {
	var record WebhookSecret
	err := app.Database.WithContext(ctx).
		Where("provider = ? AND enabled = ?", provider, true).
		First(&record).Error
	if err != nil {
		return "", err
	}
	return record.Secret, nil
}

func (app *App) upsertWebhookSecret(ctx context.Context, provider, secret string) error {
	db := app.Database.WithContext(ctx)
	var record WebhookSecret
	err := db.Where("provider = ?", provider).First(&record).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		record = WebhookSecret{Provider: provider}
	}
	record.Secret = secret
	record.Enabled = true
	if record.ID == 0 {
		return db.Create(&record).Error
	}
	return db.Save(&record).Error
}

func (app *App) isWebhookConfigured(ctx context.Context, provider string) bool {
	_, err := app.getWebhookSecret(ctx, provider)
	return err == nil
}

func documentHasTag(document Document, tag string) bool {
	if strings.TrimSpace(tag) == "" {
		return false
	}
	for _, existing := range document.Tags {
		if strings.EqualFold(existing, tag) {
			return true
		}
	}
	return false
}

func (app *App) startSuggestionWorker(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := app.recoverStaleSuggestionJobs(ctx); err != nil {
					log.WithError(err).Warn("Suggestion worker failed to recover stale jobs")
				}
				if err := app.processNextSuggestionJob(ctx); err != nil {
					log.WithError(err).Warn("Suggestion worker failed to process a job")
				}
			}
		}
	}()
}

func (app *App) processNextSuggestionJob(ctx context.Context) error {
	db := app.Database.WithContext(ctx)
	var job SuggestionJob
	err := db.Where("status IN ? AND next_attempt_at <= ?", []string{suggestionJobStatusPending, suggestionJobStatusFailed}, time.Now()).
		Order("created_at ASC").
		First(&job).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}

	now := time.Now()
	job.Status = suggestionJobStatusRunning
	job.StartedAt = &now
	if err := db.Save(&job).Error; err != nil {
		return err
	}

	document, err := app.Client.GetDocument(ctx, job.DocumentID)
	if err == nil {
		settingsMutex.RLock()
		generateCustomFields := settings.CustomFieldsEnable
		settingsMutex.RUnlock()
		req := GenerateSuggestionsRequest{
			Documents:              []Document{document},
			GenerateTitles:         strings.ToLower(autoGenerateTitle) != "false",
			GenerateTags:           strings.ToLower(autoGenerateTags) != "false",
			GenerateCorrespondents: strings.ToLower(autoGenerateCorrespondents) != "false",
			GenerateDocumentTypes:  strings.ToLower(autoGenerateDocumentType) != "false",
			GenerateCreatedDate:    strings.ToLower(autoGenerateCreatedDate) != "false",
			GenerateCustomFields:   generateCustomFields,
		}
		_, err = app.generateDocumentSuggestionsCached(ctx, req, documentLogger(job.DocumentID))
	}

	finished := time.Now()
	job.FinishedAt = &finished
	if err != nil {
		job.Status = suggestionJobStatusFailed
		job.AttemptCount++
		job.LastError = err.Error()
		backoff := time.Duration(job.AttemptCount+1) * time.Minute
		if backoff > suggestionJobMaxRetryBackoff {
			backoff = suggestionJobMaxRetryBackoff
		}
		job.NextAttemptAt = time.Now().Add(backoff)
	} else {
		job.Status = suggestionJobStatusSucceeded
		job.LastError = ""
		job.NextAttemptAt = time.Now()
	}
	return db.Save(&job).Error
}

func (app *App) recoverStaleSuggestionJobs(ctx context.Context) error {
	cutoff := time.Now().Add(-suggestionJobStaleAfter)
	return app.Database.WithContext(ctx).
		Model(&SuggestionJob{}).
		Where("status = ? AND started_at IS NOT NULL AND started_at < ?", suggestionJobStatusRunning, cutoff).
		Updates(map[string]interface{}{
			"status":          suggestionJobStatusPending,
			"started_at":      nil,
			"next_attempt_at": time.Now(),
			"last_error":      "worker stopped before finishing; retrying",
		}).Error
}
