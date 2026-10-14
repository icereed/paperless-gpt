package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PendingReview holds the suggestions a workflow with require_approval made
// for a document, until the user applies or discards them. While a document
// waits, it carries MANUAL_TAG instead of the workflow's trigger tag: that
// takes it out of the workflow's poll, so it is processed once, and shows in
// paperless-ngx that it waits for review in paperless-gpt.
type PendingReview struct {
	DocumentID   int `gorm:"primaryKey;autoIncrement:false"`
	WorkflowID   string
	WorkflowName string
	// Suggestion is the DocumentSuggestion as JSON, including the tag
	// handover (trigger and MANUAL_TAG off, completion tag on) for the apply.
	Suggestion string
	CreatedAt  time.Time
}

// pendingReviewResponse is one entry of GET /api/reviews: a suggestion the
// review UI can show and apply as is, plus where it came from.
type pendingReviewResponse struct {
	DocumentSuggestion
	WorkflowID   string    `json:"workflow_id"`
	WorkflowName string    `json:"workflow_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// holdForApproval stores a workflow's suggestion for review and swaps the
// trigger tag for MANUAL_TAG. The record is saved first: if the tag swap
// fails, the trigger stays, the next poll generates again and the upsert
// replaces the record, so a document never waits without its suggestion.
func (app *App) holdForApproval(ctx context.Context, document Document, suggestion DocumentSuggestion, workflow WorkflowConfig, triggerTag string, docLogger *logrus.Entry) error {
	data, err := json.Marshal(suggestion)
	if err != nil {
		return fmt.Errorf("encoding suggestion for document %d: %w", document.ID, err)
	}
	record := PendingReview{
		DocumentID:   document.ID,
		WorkflowID:   workflow.ID,
		WorkflowName: workflow.Name,
		Suggestion:   string(data),
		CreatedAt:    time.Now(),
	}
	if err := app.Database.Clauses(clause.OnConflict{UpdateAll: true}).Create(&record).Error; err != nil {
		return fmt.Errorf("saving suggestion for review for document %d: %w", document.ID, err)
	}
	park := DocumentSuggestion{
		ID:               document.ID,
		OriginalDocument: document,
		RemoveTags:       []string{triggerTag},
		AddTags:          []string{manualTag},
	}
	if err := app.Client.UpdateDocuments(ctx, []DocumentSuggestion{park}, app.Database, false); err != nil {
		return fmt.Errorf("moving document %d to the review queue: %w", document.ID, err)
	}
	docLogger.Infof("Suggestions wait for approval (workflow %q); %q replaced by %q", workflow.ID, triggerTag, manualTag)
	return nil
}

// pendingSuggestion rebuilds the suggestion for review against the document
// as it is now: the review shows current values, and the apply starts from
// the current tags, so tags the user changed in paperless-ngx meanwhile are
// kept.
func pendingSuggestion(record PendingReview, current Document) (DocumentSuggestion, error) {
	var suggestion DocumentSuggestion
	if err := json.Unmarshal([]byte(record.Suggestion), &suggestion); err != nil {
		return DocumentSuggestion{}, err
	}
	snapshot := suggestion.OriginalDocument
	suggestion.OriginalDocument = current
	if len(suggestion.SuggestedTags) > 0 && !suggestion.KeepOriginalTags {
		// The suggested tags include the tags the document had at generation
		// time; carry over what was added since.
		for _, tag := range current.Tags {
			if !slices.Contains(snapshot.Tags, tag) && !slices.Contains(suggestion.SuggestedTags, tag) {
				suggestion.SuggestedTags = append(suggestion.SuggestedTags, tag)
			}
		}
	}
	return suggestion, nil
}

func hasTag(tags []string, tag string) bool {
	return slices.ContainsFunc(tags, func(t string) bool { return strings.EqualFold(t, tag) })
}

// listPendingReviewsHandler handles GET /api/reviews. Records whose document
// is gone, or no longer carries MANUAL_TAG (the user took it out of the queue
// in paperless-ngx), are dropped.
func (app *App) listPendingReviewsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var records []PendingReview
	if err := app.Database.Order("created_at").Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error loading reviews: %v", err)})
		return
	}
	reviews := make([]pendingReviewResponse, 0, len(records))
	for _, record := range records {
		current, err := app.Client.GetDocument(ctx, record.DocumentID)
		if errors.Is(err, ErrDocumentNotFound) {
			app.Database.Delete(&PendingReview{}, record.DocumentID)
			continue
		}
		if err != nil {
			// Possibly transient; keep the record and show the rest.
			documentLogger(record.DocumentID).WithError(err).Warn("Could not fetch a document waiting for approval")
			continue
		}
		if !hasTag(current.Tags, manualTag) {
			documentLogger(record.DocumentID).Infof("%q was removed in paperless-ngx; dropping the suggestions waiting for approval", manualTag)
			app.Database.Delete(&PendingReview{}, record.DocumentID)
			continue
		}
		suggestion, err := pendingSuggestion(record, current)
		if err != nil {
			documentLogger(record.DocumentID).WithError(err).Error("Dropping an unreadable review record")
			app.Database.Delete(&PendingReview{}, record.DocumentID)
			continue
		}
		reviews = append(reviews, pendingReviewResponse{
			DocumentSuggestion: suggestion,
			WorkflowID:         record.WorkflowID,
			WorkflowName:       record.WorkflowName,
			CreatedAt:          record.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, reviews)
}

// discardPendingReviewHandler handles POST /api/reviews/:id/discard: the
// suggestions are dropped and MANUAL_TAG comes off, without writing anything
// else or adding the completion tag.
func (app *App) discardPendingReviewHandler(c *gin.Context) {
	ctx := c.Request.Context()
	documentID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}
	var record PendingReview
	if err := app.Database.First(&record, documentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Nothing waits for approval for this document"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	current, err := app.Client.GetDocument(ctx, documentID)
	if err != nil && !errors.Is(err, ErrDocumentNotFound) {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Error fetching document %d: %v", documentID, err)})
		return
	}
	if err == nil && hasTag(current.Tags, manualTag) {
		release := DocumentSuggestion{ID: documentID, OriginalDocument: current, RemoveTags: []string{manualTag}}
		if err := app.Client.UpdateDocuments(ctx, []DocumentSuggestion{release}, app.Database, false); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error removing %q: %v", manualTag, err)})
			return
		}
	}
	app.Database.Delete(&PendingReview{}, documentID)
	documentLogger(documentID).Infof("Suggestions of workflow %q discarded", record.WorkflowID)
	c.Status(http.StatusOK)
}

// clearPendingReviews drops the waiting suggestions of documents that were
// just updated through the review, so they leave the approval queue.
func clearPendingReviews(db *gorm.DB, documents []DocumentSuggestion) {
	if db == nil || len(documents) == 0 {
		return
	}
	ids := make([]int, 0, len(documents))
	for _, d := range documents {
		ids = append(ids, d.ID)
	}
	if err := db.Where("document_id IN ?", ids).Delete(&PendingReview{}).Error; err != nil {
		log.WithError(err).Warn("Could not clear suggestions waiting for approval")
	}
}
