package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (app *App) splitAvailable(c *gin.Context) bool {
	if app.splitConfig == nil || app.Database == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Split processing unavailable"})
		return false
	}
	return true
}

func splitRequestID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return 0, false
	}
	return uint(id), true
}

func (app *App) listSplitSuggestionsHandler(c *gin.Context) {
	if !app.splitAvailable(c) {
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 1000000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page"})
		return
	}
	query := app.Database.WithContext(c.Request.Context()).Model(&SplitSuggestion{})
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot list split suggestions"})
		return
	}
	suggestions := []SplitSuggestion{}
	if err := query.Order("id DESC").Offset((page - 1) * 50).Limit(50).Find(&suggestions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot list split suggestions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": suggestions, "count": total, "page": page, "config": app.splitConfig})
}

func (app *App) generateSplitSuggestionHandler(c *gin.Context) {
	if !app.splitAvailable(c) {
		return
	}
	id, ok := splitRequestID(c)
	if !ok {
		return
	}
	app.splitMu.Lock()
	defer app.splitMu.Unlock()
	suggestion, err := app.generateSplitSuggestion(c.Request.Context(), int(id), true)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "suggestion": suggestion})
		return
	}
	c.JSON(http.StatusOK, suggestion)
}

func (app *App) applySplitSuggestionHandler(c *gin.Context) {
	if !app.splitAvailable(c) {
		return
	}
	id, ok := splitRequestID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var request struct {
		Boundaries []int `json:"boundaries" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provide boundaries as an array of page numbers"})
		return
	}
	app.splitMu.Lock()
	defer app.splitMu.Unlock()
	suggestion, err := app.applySplitSuggestion(c.Request.Context(), id, request.Boundaries)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "suggestion": suggestion})
		return
	}
	c.JSON(http.StatusOK, suggestion)
}

func (app *App) skipSplitSuggestionHandler(c *gin.Context) {
	if !app.splitAvailable(c) {
		return
	}
	id, ok := splitRequestID(c)
	if !ok {
		return
	}
	app.splitMu.Lock()
	defer app.splitMu.Unlock()
	result := app.Database.WithContext(c.Request.Context()).Model(&SplitSuggestion{}).Where("id = ? AND status IN ?", id, []string{"review", "skipped"}).Update("status", "skipped")
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot skip split suggestion"})
		return
	}
	if result.RowsAffected != 1 {
		c.JSON(http.StatusConflict, gin.H{"error": "Only a suggestion awaiting Review can be skipped"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "skipped"})
}
