package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	bricoproHQAPIVersion = "v1"
	bricoproHQAPIKeyName = "default"
	bricoproHQAPIPrefix  = "/api/bricoprohq/v1"
)

type bricoproHQConnectorStatusResponse struct {
	Configured   bool   `json:"configured"`
	BaseURL      string `json:"base_url"`
	LocalBaseURL string `json:"local_base_url,omitempty"`
	HealthURL    string `json:"health_url"`
	DocumentsURL string `json:"documents_url"`
	HeaderName   string `json:"header_name"`
	GeneratedKey string `json:"api_key,omitempty"`
	QueueTag     string `json:"queue_tag"`
	APIVersion   string `json:"api_version"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
}

type bricoproHQDocumentListResponse struct {
	Count     int        `json:"count"`
	Limit     int        `json:"limit"`
	Documents []Document `json:"documents"`
}

func (app *App) registerBricoproHQConnectorSettingsRoutes(api *gin.RouterGroup) {
	api.GET("/bricoprohq-connector", app.getBricoproHQConnectorSettingsHandler)
	api.POST("/bricoprohq-connector/key", app.generateBricoproHQAPIKeyHandler)
	api.DELETE("/bricoprohq-connector/key", app.revokeBricoproHQAPIKeyHandler)
}

func (app *App) registerBricoproHQAPIRoutes(api *gin.RouterGroup) {
	api.GET("/health", app.bricoproHQHealthHandler)
	api.GET("/documents", app.bricoproHQDocumentsHandler)
	api.GET("/documents/:id", app.bricoproHQDocumentHandler)
}

func (app *App) bricoproHQAPIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected, err := app.bricoproHQAPIKey(c.Request.Context())
		if err != nil {
			log.WithError(err).Warn("failed to load BricoproHQ connector API key")
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to load connector API key"})
			return
		}
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "BricoproHQ connector is disabled. Generate an API key in Settings."})
			return
		}

		provided := strings.TrimSpace(c.GetHeader("X-API-Key"))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or missing API key"})
			return
		}

		go app.recordBricoproHQAPIKeyUse(context.Background())
		c.Next()
	}
}

func bricoproHQCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isBricoproHQAPIPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "X-API-Key, Content-Type")
		c.Header("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func isBricoproHQAPIPath(path string) bool {
	return path == bricoproHQAPIPrefix || strings.HasPrefix(path, bricoproHQAPIPrefix+"/")
}

func (app *App) getBricoproHQConnectorSettingsHandler(c *gin.Context) {
	status, err := app.bricoproHQConnectorStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load BricoproHQ connector status"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func (app *App) generateBricoproHQAPIKeyHandler(c *gin.Context) {
	key, err := generateBricoproHQAPIKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate API key"})
		return
	}
	encrypted, err := EncryptSecret(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt API key"})
		return
	}
	if err := app.upsertBricoproHQAPIKey(c.Request.Context(), encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save API key"})
		return
	}
	status, err := app.bricoproHQConnectorStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API key saved but status could not be loaded"})
		return
	}
	status.GeneratedKey = key
	c.JSON(http.StatusCreated, status)
}

func (app *App) revokeBricoproHQAPIKeyHandler(c *gin.Context) {
	if app == nil || app.Database == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database is not configured"})
		return
	}
	if err := app.Database.WithContext(c.Request.Context()).Where("name = ?", bricoproHQAPIKeyName).Delete(&BricoproHQAPIKey{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke API key"})
		return
	}
	status, err := app.bricoproHQConnectorStatus(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API key revoked but status could not be loaded"})
		return
	}
	c.JSON(http.StatusOK, status)
}

func generateBricoproHQAPIKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "pgpt_bhq_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (app *App) bricoproHQAPIKey(ctx context.Context) (string, error) {
	if app == nil || app.Database == nil || !app.Database.Migrator().HasTable(&BricoproHQAPIKey{}) {
		return "", nil
	}
	var record BricoproHQAPIKey
	err := app.Database.WithContext(ctx).First(&record, "name = ?", bricoproHQAPIKeyName).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !record.Enabled {
		return "", nil
	}
	return DecryptSecret(record.EncryptedKey)
}

func (app *App) upsertBricoproHQAPIKey(ctx context.Context, encryptedKey string) error {
	if app == nil || app.Database == nil {
		return errors.New("database is not configured")
	}
	var record BricoproHQAPIKey
	err := app.Database.WithContext(ctx).First(&record, "name = ?", bricoproHQAPIKeyName).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		record = BricoproHQAPIKey{Name: bricoproHQAPIKeyName}
	} else if err != nil {
		return err
	}
	record.EncryptedKey = encryptedKey
	record.Enabled = true
	record.LastUsedAt = nil
	return app.Database.WithContext(ctx).Save(&record).Error
}

func (app *App) recordBricoproHQAPIKeyUse(ctx context.Context) {
	if app == nil || app.Database == nil {
		return
	}
	now := time.Now().UTC()
	if err := app.Database.WithContext(ctx).Model(&BricoproHQAPIKey{}).Where("name = ?", bricoproHQAPIKeyName).Update("last_used_at", &now).Error; err != nil {
		log.WithError(err).Debug("failed to record BricoproHQ connector API key usage")
	}
}

func (app *App) bricoproHQConnectorStatus(c *gin.Context) (bricoproHQConnectorStatusResponse, error) {
	baseURL := strings.TrimRight(currentBaseURL(c), "/")
	localBaseURL := localBricoproHQBaseURL(c)
	status := bricoproHQConnectorStatusResponse{
		BaseURL:      baseURL,
		LocalBaseURL: localBaseURL,
		HealthURL:    baseURL + bricoproHQAPIPrefix + "/health",
		DocumentsURL: baseURL + bricoproHQAPIPrefix + "/documents",
		HeaderName:   "X-API-Key",
		QueueTag:     manualTag,
		APIVersion:   bricoproHQAPIVersion,
	}
	if localBaseURL != "" {
		status.HealthURL = localBaseURL + bricoproHQAPIPrefix + "/health"
		status.DocumentsURL = localBaseURL + bricoproHQAPIPrefix + "/documents"
	}
	if app == nil || app.Database == nil || !app.Database.Migrator().HasTable(&BricoproHQAPIKey{}) {
		return status, nil
	}
	var record BricoproHQAPIKey
	err := app.Database.WithContext(c.Request.Context()).First(&record, "name = ?", bricoproHQAPIKeyName).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Configured = record.Enabled && record.EncryptedKey != ""
	if record.LastUsedAt != nil {
		status.LastUsedAt = record.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return status, nil
}

func localBricoproHQBaseURL(c *gin.Context) string {
	origin := strings.TrimRight(strings.TrimSpace(c.GetHeader("Origin")), "/")
	if origin == "" || !isAllowedBricoproHQLocalOrigin(origin) {
		baseURL := strings.TrimRight(currentBaseURL(c), "/")
		if isAllowedBricoproHQLocalOrigin(baseURL) {
			return baseURL
		}
		return ""
	}

	serverURL := strings.TrimRight(currentBaseURL(c), "/")
	serverHostPort := ""
	if parsed, err := url.Parse(serverURL); err == nil {
		serverHostPort = parsed.Port()
	}
	if serverHostPort == "" {
		return origin
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	host := parsedOrigin.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s:%s", parsedOrigin.Scheme, host, serverHostPort)
}

func isAllowedBricoproHQLocalOrigin(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return false
}

func (app *App) bricoproHQHealthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"service":     "paperless-gpt",
		"version":     version,
		"api_version": bricoproHQAPIVersion,
	})
}

func (app *App) bricoproHQDocumentsHandler(c *gin.Context) {
	limit := parsePositiveIntQuery(c, "limit", 25, 100)
	documents, err := app.Client.GetDocumentsByTag(c.Request.Context(), manualTag, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching pending documents: %v", err)})
		return
	}
	c.JSON(http.StatusOK, bricoproHQDocumentListResponse{
		Count:     len(documents),
		Limit:     limit,
		Documents: documents,
	})
}

func (app *App) bricoproHQDocumentHandler(c *gin.Context) {
	documentID, err := strconv.Atoi(c.Param("id"))
	if err != nil || documentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	document, err := app.Client.GetDocument(c.Request.Context(), documentID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching document: %v", err)})
		return
	}
	c.JSON(http.StatusOK, document)
}

func parsePositiveIntQuery(c *gin.Context, name string, defaultValue, maxValue int) int {
	value := defaultValue
	if raw := strings.TrimSpace(c.Query(name)); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			value = parsed
		}
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
