package main

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// getEmbeddingsHandler handles GET /api/embeddings.
func (app *App) getEmbeddingsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, app.embeddingStatus(c.Request.Context()))
}

// buildEmbeddingsHandler handles POST /api/embeddings/build.
func (app *App) buildEmbeddingsHandler(c *gin.Context) {
	app.startEmbeddingsHandler(c, "build")
}

// rebuildEmbeddingsHandler handles POST /api/embeddings/rebuild.
func (app *App) rebuildEmbeddingsHandler(c *gin.Context) {
	app.startEmbeddingsHandler(c, "rebuild")
}

func (app *App) startEmbeddingsHandler(c *gin.Context, mode string) {
	if err := app.startEmbeddingJob(mode); err != nil {
		switch {
		case errors.Is(err, errEmbeddingsDisabled):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, errEmbeddingJobRunning):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusAccepted, app.embeddingStatus(c.Request.Context()))
}

// cancelEmbeddingsHandler handles POST /api/embeddings/cancel.
func (app *App) cancelEmbeddingsHandler(c *gin.Context) {
	app.cancelEmbeddingJob()
	c.JSON(http.StatusOK, app.embeddingStatus(c.Request.Context()))
}
