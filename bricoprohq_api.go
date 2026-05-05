package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
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
	StatsURL     string `json:"stats_url"`
	HeaderName   string `json:"header_name"`
	GeneratedKey string `json:"api_key,omitempty"`
	APIVersion   string `json:"api_version"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
}

type bricoproHQStatsResponse struct {
	APIVersion                                   string                           `json:"api_version"`
	GeneratedAt                                  string                           `json:"generated_at"`
	WindowDays                                   int                              `json:"window_days"`
	Queue                                        bricoproHQQueueStats             `json:"queue"`
	ProcessedDocumentsLast30Days                 int64                            `json:"processed_documents_last_30_days"`
	MostUsedTagsLast30Days                       []bricoproHQTagStat              `json:"most_used_tags_last_30_days"`
	HighestCustomFieldAmountSuggestionLast30Days *bricoproHQCustomFieldAmountStat `json:"highest_custom_field_amount_suggestion_last_30_days"`
}

type bricoproHQQueueStats struct {
	Pending int64 `json:"pending"`
	Running int64 `json:"running"`
	Failed  int64 `json:"failed"`
	Total   int64 `json:"total"`
}

type bricoproHQTagStat struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type bricoproHQCustomFieldAmountStat struct {
	FieldID     int     `json:"field_id,omitempty"`
	FieldName   string  `json:"field_name,omitempty"`
	Amount      float64 `json:"amount"`
	Value       string  `json:"value,omitempty"`
	GeneratedAt string  `json:"generated_at"`
}

func (app *App) registerBricoproHQConnectorSettingsRoutes(api *gin.RouterGroup) {
	api.GET("/bricoprohq-connector", app.getBricoproHQConnectorSettingsHandler)
	api.POST("/bricoprohq-connector/key", app.generateBricoproHQAPIKeyHandler)
	api.DELETE("/bricoprohq-connector/key", app.revokeBricoproHQAPIKeyHandler)
}

func (app *App) registerBricoproHQAPIRoutes(api *gin.RouterGroup) {
	api.GET("/health", app.bricoproHQHealthHandler)
	api.GET("/stats", app.bricoproHQStatsHandler)
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
		StatsURL:     baseURL + bricoproHQAPIPrefix + "/stats",
		HeaderName:   "X-API-Key",
		APIVersion:   bricoproHQAPIVersion,
	}
	if localBaseURL != "" {
		status.HealthURL = localBaseURL + bricoproHQAPIPrefix + "/health"
		status.StatsURL = localBaseURL + bricoproHQAPIPrefix + "/stats"
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

func (app *App) bricoproHQStatsHandler(c *gin.Context) {
	stats, err := app.bricoproHQStats(c.Request.Context(), time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error building Paperless GPT stats: %v", err)})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (app *App) bricoproHQStats(ctx context.Context, now time.Time) (bricoproHQStatsResponse, error) {
	if app == nil || app.Database == nil {
		return bricoproHQStatsResponse{}, errors.New("database is not configured")
	}
	windowDays := 30
	cutoff := now.AddDate(0, 0, -windowDays)
	db := app.Database.WithContext(ctx)

	stats := bricoproHQStatsResponse{
		APIVersion:             bricoproHQAPIVersion,
		GeneratedAt:            now.Format(time.RFC3339),
		WindowDays:             windowDays,
		MostUsedTagsLast30Days: []bricoproHQTagStat{},
	}

	if err := db.Model(&SuggestionJob{}).
		Where("status = ?", suggestionJobStatusPending).
		Count(&stats.Queue.Pending).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&SuggestionJob{}).
		Where("status = ?", suggestionJobStatusRunning).
		Count(&stats.Queue.Running).Error; err != nil {
		return stats, err
	}
	if err := db.Model(&SuggestionJob{}).
		Where("status = ?", suggestionJobStatusFailed).
		Count(&stats.Queue.Failed).Error; err != nil {
		return stats, err
	}
	stats.Queue.Total = stats.Queue.Pending + stats.Queue.Running + stats.Queue.Failed

	if err := db.Model(&DocumentSuggestionCache{}).
		Where("generated_at >= ?", cutoff).
		Distinct("document_id").
		Count(&stats.ProcessedDocumentsLast30Days).Error; err != nil {
		return stats, err
	}

	var cachedSuggestions []DocumentSuggestionCache
	if err := db.Select("id", "generated_at", "suggestions_json").
		Where("generated_at >= ?", cutoff).
		Find(&cachedSuggestions).Error; err != nil {
		return stats, err
	}

	tagCounts := map[string]int{}
	var highest *bricoproHQCustomFieldAmountStat
	for _, cache := range cachedSuggestions {
		var suggestion DocumentSuggestion
		if err := json.Unmarshal([]byte(cache.SuggestionsJSON), &suggestion); err != nil {
			log.WithError(err).WithField("cache_id", cache.ID).Debug("failed to parse cached suggestion for BricoproHQ stats")
			continue
		}
		for _, tag := range suggestion.SuggestedTags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tagCounts[tag]++
			}
		}
		for _, field := range suggestion.SuggestedCustomFields {
			if !isBricoproHQAmountField(field.Name) {
				continue
			}
			amount, ok := parseNumericValue(field.Value)
			if !ok {
				continue
			}
			if highest == nil || amount > highest.Amount {
				highest = &bricoproHQCustomFieldAmountStat{
					FieldID:     field.ID,
					FieldName:   strings.TrimSpace(field.Name),
					Amount:      amount,
					Value:       stringifyExpenseFieldValue(field.Value),
					GeneratedAt: cache.GeneratedAt.UTC().Format(time.RFC3339),
				}
			}
		}
	}
	stats.MostUsedTagsLast30Days = topBricoproHQTags(tagCounts, 10)
	stats.HighestCustomFieldAmountSuggestionLast30Days = highest

	return stats, nil
}

func topBricoproHQTags(tagCounts map[string]int, limit int) []bricoproHQTagStat {
	tags := make([]bricoproHQTagStat, 0, len(tagCounts))
	for tag, count := range tagCounts {
		tags = append(tags, bricoproHQTagStat{Tag: tag, Count: count})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Count != tags[j].Count {
			return tags[i].Count > tags[j].Count
		}
		return tags[i].Tag < tags[j].Tag
	})
	if limit > 0 && len(tags) > limit {
		return tags[:limit]
	}
	return tags
}

func isBricoproHQAmountField(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(normalized, "amount") ||
		strings.Contains(normalized, "total") ||
		strings.Contains(normalized, "price") ||
		strings.Contains(normalized, "cost")
}
