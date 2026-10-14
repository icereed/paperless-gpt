package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"text/template"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPendingReviewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ModificationHistory{}, &PendingReview{}))
	return db
}

// approvalClient is a recordingClient that can also serve the catalogs
// suggestion generation reads.
type approvalClient struct{ recordingClient }

func (c *approvalClient) GetAllCorrespondents(context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}
func (c *approvalClient) GetAllDocumentTypes(context.Context) ([]DocumentType, error) {
	return nil, nil
}

// approvalWorkflow sets up a workflow that only generates titles and waits
// for approval.
func approvalWorkflow(t *testing.T, trigger string) {
	t.Helper()
	prevAutoTag, prevManualTag, prevTitle := autoTag, manualTag, titleTemplate
	t.Cleanup(func() { autoTag, manualTag, titleTemplate = prevAutoTag, prevManualTag, prevTitle })
	autoTag, manualTag = "paperless-gpt-auto", "paperless-gpt"
	titleTemplate = template.Must(template.New("title").Parse("{{.Content}}"))
	isolateWorkflowSettings(t)
	on, off := true, false
	useWorkflows(t, WorkflowConfig{
		ID: "invoices", Name: "Invoices", TriggerTag: trigger, CompletionTag: "invoices-done",
		GenerateTitles: &on, GenerateTags: &off, GenerateCorrespondents: &off,
		GenerateCreatedDate: &off, GenerateDocumentTypes: &off, GenerateCustomFields: &off,
		RequireApproval: true,
	})
}

func TestApprovalWorkflowHoldsSuggestionsInsteadOfWriting(t *testing.T) {
	const trigger = "invoices-in"
	approvalWorkflow(t, trigger)

	client := &approvalClient{recordingClient{taggedDocuments: map[string][]Document{
		trigger: {{ID: 41, Title: "scan_0041.pdf", Content: "ACME invoice", Tags: []string{trigger, "mine"}}},
	}}}
	db := newPendingReviewTestDB(t)
	app := &App{Client: client, Database: db, LLM: &mockLLM{Response: "ACME Invoice"}}

	count, err := app.processAutoTagDocuments(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	require.Len(t, client.calls, 1, "only the tag swap is written")
	park := client.calls[0]
	assert.Empty(t, park.SuggestedTitle, "nothing but tags may be written before approval")
	assert.Equal(t, []string{trigger}, park.RemoveTags)
	assert.Equal(t, []string{manualTag}, park.AddTags)

	var record PendingReview
	require.NoError(t, db.First(&record, 41).Error)
	assert.Equal(t, "invoices", record.WorkflowID)
	assert.Equal(t, "Invoices", record.WorkflowName)
	var held DocumentSuggestion
	require.NoError(t, json.Unmarshal([]byte(record.Suggestion), &held))
	assert.Equal(t, "ACME Invoice", held.SuggestedTitle)
	assert.Contains(t, held.RemoveTags, trigger)
	assert.Contains(t, held.RemoveTags, manualTag, "approving takes the document out of the review queue")
	assert.Equal(t, []string{"invoices-done"}, held.AddTags, "the completion tag is only added on approval")
}

func TestApprovalWorkflowFailedSwapLeavesNoRecord(t *testing.T) {
	const trigger = "invoices-in"
	approvalWorkflow(t, trigger)
	prevFail := failTag
	t.Cleanup(func() { failTag = prevFail })
	failTag = "paperless-gpt-failed"

	client := &approvalClient{recordingClient{
		taggedDocuments: map[string][]Document{trigger: {{ID: 42, Title: "x", Content: "y", Tags: []string{trigger}}}},
		updateErr:       assert.AnError,
		failAfterNCalls: 1,
	}}
	db := newPendingReviewTestDB(t)
	app := &App{Client: client, Database: db, LLM: &mockLLM{Response: "Title"}}

	_, err := app.processAutoTagDocuments(context.Background())
	require.Error(t, err)
	var n int64
	db.Model(&PendingReview{}).Count(&n)
	assert.Zero(t, n, "a document that never reached the queue must not leave a suggestion behind")
	require.Len(t, client.calls, 2)
	assert.Equal(t, []string{trigger}, client.calls[1].RemoveTags, "the trigger comes off with the fail tag")
}

func TestPendingSuggestionUsesCurrentDocument(t *testing.T) {
	held := DocumentSuggestion{
		ID:               7,
		OriginalDocument: Document{ID: 7, Title: "old", Tags: []string{"invoices-in", "mine"}},
		SuggestedTitle:   "New",
		SuggestedTags:    []string{"invoices-in", "mine", "finance"},
	}
	data, _ := json.Marshal(held)
	current := Document{ID: 7, Title: "edited by hand", Tags: []string{"mine", "paperless-gpt", "urgent"}}

	got, err := pendingSuggestion(PendingReview{DocumentID: 7, Suggestion: string(data)}, current)
	require.NoError(t, err)
	assert.Equal(t, "edited by hand", got.OriginalDocument.Title, "the review compares against current values")
	assert.Contains(t, got.SuggestedTags, "urgent", "a tag added while waiting is kept")
	assert.Contains(t, got.SuggestedTags, "paperless-gpt")
}

func TestPendingReviewHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevManual := manualTag
	t.Cleanup(func() { manualTag = prevManual })
	manualTag = "paperless-gpt"

	held, _ := json.Marshal(DocumentSuggestion{ID: 51, SuggestedTitle: "Held", RemoveTags: []string{manualTag}})
	db := newPendingReviewTestDB(t)
	for _, id := range []int{51, 52, 53} {
		require.NoError(t, db.Create(&PendingReview{DocumentID: id, WorkflowID: "invoices", WorkflowName: "Invoices", Suggestion: string(held)}).Error)
	}
	client := &recordingClient{taggedDocuments: map[string][]Document{
		manualTag: {
			{ID: 51, Title: "waiting", Tags: []string{manualTag}},
			{ID: 52, Title: "taken out by hand", Tags: []string{"mine"}},
		},
	}}
	app := &App{Client: client, Database: db}
	router := gin.New()
	router.GET("/api/reviews", app.listPendingReviewsHandler)
	router.POST("/api/reviews/:id/discard", app.discardPendingReviewHandler)

	t.Run("list", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/reviews", nil))
		require.Equal(t, http.StatusOK, w.Code)
		var got []pendingReviewResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Len(t, got, 1, "52 left the queue; 53 could not be fetched")
		assert.Equal(t, 51, got[0].ID)
		assert.Equal(t, "Invoices", got[0].WorkflowName)
		assert.Equal(t, "Held", got[0].SuggestedTitle)
		assert.Equal(t, "waiting", got[0].OriginalDocument.Title)
		var n int64
		db.Model(&PendingReview{}).Where("document_id = ?", 52).Count(&n)
		assert.Zero(t, n, "removing the review tag in paperless-ngx discards the suggestion")
		db.Model(&PendingReview{}).Where("document_id = ?", 53).Count(&n)
		assert.Equal(t, int64(1), n, "a fetch error may be transient and keeps the suggestion")
	})
	db.Delete(&PendingReview{}, 53)

	t.Run("discard", func(t *testing.T) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/reviews/51/discard", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Len(t, client.calls, 1)
		release := client.calls[0]
		assert.Equal(t, []string{manualTag}, release.RemoveTags)
		assert.Empty(t, release.AddTags, "a discarded document never gets the completion tag")
		assert.Empty(t, release.SuggestedTitle)
		var n int64
		db.Model(&PendingReview{}).Count(&n)
		assert.Zero(t, n)

		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/reviews/51/discard", nil))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestClearPendingReviewsAfterApply(t *testing.T) {
	db := newPendingReviewTestDB(t)
	require.NoError(t, db.Create(&PendingReview{DocumentID: 61, Suggestion: "{}"}).Error)
	require.NoError(t, db.Create(&PendingReview{DocumentID: 62, Suggestion: "{}"}).Error)

	clearPendingReviews(db, []DocumentSuggestion{{ID: 61}})

	var ids []int
	db.Model(&PendingReview{}).Pluck("document_id", &ids)
	assert.Equal(t, []int{62}, ids)
}
