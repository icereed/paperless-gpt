package main

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// GetDocumentsApiResponse is the response payload for /documents endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentsApiResponse struct {
	Count int `json:"count"`
	// Next     interface{} `json:"next"`
	// Previous interface{} `json:"previous"`
	All     []int                          `json:"all"`
	Results []GetDocumentApiResponseResult `json:"results"`
}

// GetDocumentApiResponseResult is a part of the response payload for /documents endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentApiResponseResult struct {
	ID            int `json:"id"`
	Correspondent int `json:"correspondent"`
	// DocumentType        interface{}   `json:"document_type"`
	// StoragePath         interface{}   `json:"storage_path"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Tags    []int  `json:"tags"`
	// Created             time.Time     `json:"created"`
	CreatedDate string `json:"created_date"`
	// Modified            time.Time     `json:"modified"`
	// Added               time.Time     `json:"added"`
	// ArchiveSerialNumber interface{}   `json:"archive_serial_number"`
	OriginalFileName string               `json:"original_file_name"`
	ArchivedFileName string               `json:"archived_file_name"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
	// Owner               int           `json:"owner"`
	// UserCanChange       bool          `json:"user_can_change"`
	Notes []interface{} `json:"notes"`
	// SearchHit struct {
	// 	Score          float64 `json:"score"`
	// 	Highlights     string  `json:"highlights"`
	// 	NoteHighlights string  `json:"note_highlights"`
	// 	Rank           int     `json:"rank"`
	// } `json:"__search_hit__"`
}

// CustomFieldResponse represents a custom field with its value for a document
type CustomFieldResponse struct {
	Field int         `json:"field"`
	Value interface{} `json:"value"`
	Name  string      `json:"name,omitempty"`
}

// CustomFieldSuggestion represents a suggested custom field with its value and name
type CustomFieldSuggestion struct {
	ID    int         `json:"id"`
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// GetDocumentApiResponse is the response payload for /documents/{id} endpoint.
// But we are only interested in a subset of the fields.
type GetDocumentApiResponse struct {
	ID               int                   `json:"id"`
	Correspondent    int                   `json:"correspondent"`
	DocumentType     int                   `json:"document_type"`
	Title            string                `json:"title"`
	Content          string                `json:"content"`
	Tags             []int                 `json:"tags"`
	CreatedDate      string                `json:"created_date"`
	OriginalFileName string                `json:"original_file_name"`
	ArchivedFileName string                `json:"archived_file_name"`
	Notes            []interface{}         `json:"notes"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
}

// Document is a stripped down version of the document object from paperless-ngx.
// Response payload for /documents endpoint and part of request payload for /generate-suggestions endpoint
type Document struct {
	ID               int                   `json:"id"`
	Title            string                `json:"title"`
	Content          string                `json:"content"`
	Tags             []string              `json:"tags"`
	Correspondent    string                `json:"correspondent"`
	CreatedDate      string                `json:"created_date"`
	OriginalFileName string                `json:"original_file_name"`
	ArchivedFileName string                `json:"archived_file_name"`
	DocumentTypeName string                `json:"document_type_name"`
	CustomFields     []CustomFieldResponse `json:"custom_fields"`
}

// GenerateSuggestionsRequest is the request payload for generating suggestions for /generate-suggestions endpoint
type GenerateSuggestionsRequest struct {
	Documents              []Document `json:"documents"`
	GenerateTitles         bool       `json:"generate_titles,omitempty"`
	GenerateTags           bool       `json:"generate_tags,omitempty"`
	GenerateCorrespondents bool       `json:"generate_correspondents,omitempty"`
	GenerateCreatedDate    bool       `json:"generate_created_date,omitempty"`
	GenerateCustomFields   bool       `json:"generate_custom_fields,omitempty"`
	GenerateDocumentTypes  bool       `json:"generate_document_types,omitempty"`
}

// AnalyzeDocumentsRequest is the request payload for the ad-hoc analysis
type AnalyzeDocumentsRequest struct {
	DocumentIDs []int  `json:"document_ids"`
	Prompt      string `json:"prompt"`
}

// Settings defines the structure for server-side UI settings
type Settings struct {
	CustomFieldsEnable               bool   `json:"custom_fields_enable"`
	CustomFieldsSelectedIDs          []int  `json:"custom_fields_selected_ids"`
	CustomFieldsWriteMode            string `json:"custom_fields_write_mode"` // "append", "update", or "replace"
	RestrictTagsToExisting           bool   `json:"restrict_tags_to_existing"`
	RestrictCorrespondentsToExisting bool   `json:"restrict_correspondents_to_existing"`
	RestrictDocumentTypesToExisting  bool   `json:"restrict_document_types_to_existing"`
	JobberEnabled                    bool   `json:"jobber_enabled"`
	JobberJobIDFieldID               int    `json:"jobber_job_id_field_id"`
	JobberJobNumberFieldID           int    `json:"jobber_job_number_field_id"`
	JobberClientFieldID              int    `json:"jobber_client_field_id"`
	JobberJobNameFieldID             int    `json:"jobber_job_name_field_id"`
	JobberExpenseEnabled             bool   `json:"jobber_expense_enabled"`
	JobberExpenseTitleFieldRef       string `json:"jobber_expense_title_field_ref"`
	JobberExpenseTitleFieldID        int    `json:"jobber_expense_title_field_id"`
	JobberExpenseDescriptionFieldRef string `json:"jobber_expense_description_field_ref"`
	JobberExpenseDescriptionFieldID  int    `json:"jobber_expense_description_field_id"`
	JobberExpenseDateFieldRef        string `json:"jobber_expense_date_field_ref"`
	JobberExpenseDateFieldID         int    `json:"jobber_expense_date_field_id"`
	JobberExpenseTotalFieldRef       string `json:"jobber_expense_total_field_ref"`
	JobberExpenseTotalFieldID        int    `json:"jobber_expense_total_field_id"`
	GoogleDriveEnabled               bool   `json:"google_drive_enabled"`
	GoogleDriveFolderID              string `json:"google_drive_folder_id"`
	QuickBooksEnabled                bool   `json:"quickbooks_enabled"`
}

// DocumentSuggestion is the response payload for /generate-suggestions endpoint and the request payload for /update-documents endpoint (as an array)
type DocumentSuggestion struct {
	ID                     int                     `json:"id"`
	OriginalDocument       Document                `json:"original_document"`
	SuggestedTitle         string                  `json:"suggested_title,omitempty"`
	SuggestedTags          []string                `json:"suggested_tags,omitempty"`
	SuggestedContent       string                  `json:"suggested_content,omitempty"`
	SuggestedCorrespondent string                  `json:"suggested_correspondent,omitempty"`
	SuggestedCreatedDate   string                  `json:"suggested_created_date,omitempty"`
	SuggestedDocumentType  string                  `json:"suggested_document_type,omitempty"`
	SuggestedCustomFields  []CustomFieldSuggestion `json:"suggested_custom_fields,omitempty"`
	KeepOriginalTags       bool                    `json:"keep_original_tags,omitempty"`
	RemoveTags             []string                `json:"remove_tags,omitempty"`
	AddTags                []string                `json:"add_tags,omitempty"`
	CustomFieldsWriteMode  string                  `json:"custom_fields_write_mode,omitempty"`
	CustomFieldsEnable     bool                    `json:"custom_fields_enable"`
	JobberCandidates       []JobberMatchCandidate  `json:"jobber_candidates,omitempty"`
	SelectedJobberMatchID  string                  `json:"selected_jobber_match_id,omitempty"`
	CreateJobberExpense    bool                    `json:"create_jobber_expense,omitempty"`
	UploadToGoogleDrive    bool                    `json:"upload_to_google_drive,omitempty"`
}

type JobberMatchCandidate struct {
	ID         string `json:"id"`
	JobNumber  string `json:"job_number"`
	ClientName string `json:"client_name"`
	JobName    string `json:"job_name"`
	// StartAt is the job's scheduled start date (ISO-8601). Empty when Jobber
	// does not have a start date for the job (e.g. unscheduled work).
	StartAt string `json:"start_at,omitempty"`
	// EndAt is the job's scheduled end date (ISO-8601). Empty for open-ended jobs.
	EndAt string `json:"end_at,omitempty"`
	// CompletedAt is the date the job was marked complete (ISO-8601).
	CompletedAt string `json:"completed_at,omitempty"`
	// CreatedAt is when the job record was created in Jobber. Always present.
	CreatedAt string `json:"created_at,omitempty"`
	// MatchReason is a short human-readable explanation of why this candidate
	// was ranked where it was — useful for the UI to show "auto-matched on date".
	MatchReason string `json:"match_reason,omitempty"`
}

func (c JobberMatchCandidate) DisplayLabel() string {
	parts := []string{}
	if c.JobNumber != "" {
		parts = append(parts, "#"+c.JobNumber)
	}
	if c.ClientName != "" {
		parts = append(parts, c.ClientName)
	}
	if c.JobName != "" {
		parts = append(parts, c.JobName)
	}
	return strings.Join(parts, " - ")
}

type IntegrationConnectionStatus struct {
	Provider    string `json:"provider"`
	Configured  bool   `json:"configured"`
	Connected   bool   `json:"connected"`
	AccountName string `json:"account_name,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type DocumentIntegrationResult struct {
	DocumentID           int    `json:"document_id"`
	PaperlessUpdated     bool   `json:"paperless_updated"`
	JobberApplied        bool   `json:"jobber_applied,omitempty"`
	JobberError          string `json:"jobber_error,omitempty"`
	JobberExpenseCreated bool   `json:"jobber_expense_created,omitempty"`
	JobberExpenseID      string `json:"jobber_expense_id,omitempty"`
	JobberExpenseError   string `json:"jobber_expense_error,omitempty"`
	GoogleDriveUploaded  bool   `json:"google_drive_uploaded,omitempty"`
	GoogleDriveFileID    string `json:"google_drive_file_id,omitempty"`
	GoogleDriveURL       string `json:"google_drive_url,omitempty"`
	GoogleDriveError     string `json:"google_drive_error,omitempty"`
}

type Correspondent struct {
	Name              string `json:"name"`
	MatchingAlgorithm int    `json:"matching_algorithm"`
	Match             string `json:"match"`
	IsInsensitive     bool   `json:"is_insensitive"`
	Owner             *int   `json:"owner"`
	SetPermissions    struct {
		View struct {
			Users  []int `json:"users"`
			Groups []int `json:"groups"`
		} `json:"view"`
		Change struct {
			Users  []int `json:"users"`
			Groups []int `json:"groups"`
		} `json:"change"`
	} `json:"set_permissions"`
}

// OCROptions contains options for the OCR processing
type OCROptions struct {
	UploadPDF       bool   // Whether to upload the generated PDF
	ReplaceOriginal bool   // Whether to delete the original document after uploading
	CopyMetadata    bool   // Whether to copy metadata from the original document
	LimitPages      int    // Limit on the number of pages to process (0 = no limit)
	ProcessMode     string // OCR processing mode: "image" (default) or "pdf"
}

// ClientInterface defines the interface for PaperlessClient operations
type ClientInterface interface {
	GetDocumentsByTag(ctx context.Context, tag string, pageSize int) ([]Document, error)
	GetDocumentCountByTag(ctx context.Context, tag string) (int, error)
	UpdateDocuments(ctx context.Context, documents []DocumentSuggestion, db *gorm.DB, isUndo bool) error
	GetDocument(ctx context.Context, documentID int) (Document, error)
	GetAllTags(ctx context.Context) (map[string]int, error)
	GetAllCorrespondents(ctx context.Context) (map[string]int, error)
	GetAllDocumentTypes(ctx context.Context) ([]DocumentType, error)
	GetCustomFields(ctx context.Context) ([]CustomField, error)
	CreateTag(ctx context.Context, tagName string) (int, error)
	DownloadPDF(ctx context.Context, document Document) ([]byte, error)
	DownloadDocumentAsImages(ctx context.Context, documentID int, pageLimit int) ([]string, int, error)
	DownloadDocumentAsPDF(ctx context.Context, documentID int, limitPages int, split bool) ([]string, []byte, int, error)
	UploadDocument(ctx context.Context, data []byte, filename string, metadata map[string]interface{}) (string, error)
	UpsertDocumentCustomFields(ctx context.Context, documentID int, fieldValues map[int]interface{}, db *gorm.DB) error
	GetTaskStatus(ctx context.Context, taskID string) (map[string]interface{}, error)
	DeleteDocument(ctx context.Context, documentID int) error
}

// DocumentProcessor defines the interface for processing documents with OCR
type DocumentProcessor interface {
	ProcessDocumentOCR(ctx context.Context, documentID int, options OCROptions, jobID string) (*ProcessedDocument, error)
}
