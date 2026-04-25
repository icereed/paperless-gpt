package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ModificationHistory represents the schema of the modification_history table
type ModificationHistory struct {
	ID            uint   `gorm:"primaryKey"`             // Auto-incrementing primary key
	DocumentID    uint   `gorm:"not null"`               // Foreign key to documents table (if applicable)
	BatchID       *uint  `gorm:"index"`                  // Optional apply batch grouping multiple field changes
	DateChanged   string `gorm:"not null"`               // Date and time of modification
	ModField      string `gorm:"size:255;not null"`      // Field being modified
	PreviousValue string `gorm:"size:1048576"`           // Previous value of the field
	NewValue      string `gorm:"size:1048576"`           // New value of the field
	Undone        bool   `gorm:"not null;default:false"` // Whether the modification has been undone
	UndoneDate    string `gorm:"default:null"`           // Date and time of undoing the modification
}

type ModificationHistoryWithIntegrations struct {
	ModificationHistory
	IntegrationActions []IntegrationActionLog `json:"integration_actions,omitempty" gorm:"-"`
}

type ModificationHistoryResponse struct {
	ModificationHistory
	IntegrationActions []IntegrationActionLog `json:"IntegrationActions,omitempty"`
}

type ApplyBatch struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	StartedAt time.Time  `gorm:"index;not null" json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	DocCount  int        `json:"doc_count"`
	Summary   string     `gorm:"type:TEXT" json:"summary"`
	Undone    bool       `gorm:"not null;default:false" json:"undone"`
	UndoneAt  *time.Time `json:"undone_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type DocumentSuggestionCache struct {
	ID                    uint      `gorm:"primaryKey"`
	DocumentID            int       `gorm:"index:idx_document_suggestion_hash,unique;not null"`
	GeneratedAt           time.Time `gorm:"index;not null"`
	SourceHash            string    `gorm:"size:64;index:idx_document_suggestion_hash,unique;not null"`
	SuggestionsJSON       string    `gorm:"type:TEXT;not null"`
	JobberCandidatesJSON  string    `gorm:"type:TEXT"`
	Model                 string    `gorm:"size:255"`
	Provider              string    `gorm:"size:128"`
	PromptVersions        string    `gorm:"type:TEXT"`
	GenerationRequestJSON string    `gorm:"type:TEXT"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type IntegrationCandidateCache struct {
	ID             uint      `gorm:"primaryKey"`
	DocumentID     int       `gorm:"index:idx_integration_match_hash,unique;not null"`
	Provider       string    `gorm:"size:64;index:idx_integration_match_hash,unique;not null"`
	MatchHash      string    `gorm:"size:64;index:idx_integration_match_hash,unique;not null"`
	CandidatesJSON string    `gorm:"type:TEXT;not null"`
	AutoSelectedID string    `gorm:"size:255"`
	GeneratedAt    time.Time `gorm:"index;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SuggestionJob struct {
	ID            uint      `gorm:"primaryKey"`
	DocumentID    int       `gorm:"index;not null"`
	SourceHash    string    `gorm:"size:64;index"`
	Status        string    `gorm:"size:32;index;not null"`
	AttemptCount  int       `gorm:"not null;default:0"`
	LastError     string    `gorm:"type:TEXT"`
	NextAttemptAt time.Time `gorm:"index;not null"`
	StartedAt     *time.Time
	FinishedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WebhookSecret struct {
	ID        uint   `gorm:"primaryKey"`
	Provider  string `gorm:"size:64;uniqueIndex;not null"`
	Secret    string `gorm:"type:TEXT;not null"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type OCRPageResult struct {
	ID             uint   `gorm:"primaryKey"`
	DocumentID     int    `gorm:"index;not null"`
	PageIndex      int    `gorm:"not null"`
	Text           string `gorm:"size:1048576"`
	OcrLimitHit    bool
	GenerationInfo string `gorm:"type:TEXT"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type IntegrationReceiptShare struct {
	ID         uint      `gorm:"primaryKey"`
	Token      string    `gorm:"uniqueIndex;size:255;not null"`
	Provider   string    `gorm:"size:64;index;not null"`
	DocumentID int       `gorm:"index;not null"`
	FileName   string    `gorm:"size:512"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// InitializeDB initializes the SQLite database and migrates the schema
func InitializeDB() *gorm.DB {
	// Ensure db directory exists (owner-only: contains OAuth tokens, sessions, users)
	dbDir := "db"
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		log.Fatalf("Failed to create db directory: %v", err)
	}

	dbPath := filepath.Join(dbDir, "modification_history.db")

	// Connect to SQLite database
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Migrate the schema (create the tables if they don't exist)
	err = db.AutoMigrate(&ModificationHistory{}, &ApplyBatch{}, &DocumentSuggestionCache{}, &IntegrationCandidateCache{}, &SuggestionJob{}, &WebhookSecret{}, &OCRPageResult{}, &IntegrationConnection{}, &OAuthStateRecord{}, &IntegrationActionLog{}, &IntegrationReceiptShare{}, &ReceiptAccessToken{}, &User{}, &UserSession{})
	if err != nil {
		log.Fatalf("Failed to migrate database schema: %v", err)
	}

	return db
}

// InsertModification inserts a new modification record into the database
func InsertModification(db *gorm.DB, record *ModificationHistory) error {
	log.Debugf("Passed modification record: %+v", record)
	record.DateChanged = time.Now().Format(time.RFC3339) // Set the DateChanged field to the current time
	log.Debugf("Inserting modification record: %+v", record)
	result := db.Create(record) // GORM's Create method
	log.Debugf("Insertion result: %+v", result)
	return result.Error
}

// GetModification retrieves a modification record by its ID
func GetModification(db *gorm.DB, id uint) (*ModificationHistory, error) {
	var record ModificationHistory
	result := db.First(&record, id) // GORM's First method retrieves the first record matching the ID
	return &record, result.Error
}

// GetAllModifications retrieves all modification records from the database (deprecated - use GetPaginatedModifications instead)
func GetAllModifications(db *gorm.DB) ([]ModificationHistory, error) {
	var records []ModificationHistory
	result := db.Order("date_changed DESC").Find(&records)
	return records, result.Error
}

// GetPaginatedModifications retrieves a page of modification records with total count
func GetPaginatedModifications(db *gorm.DB, page int, pageSize int) ([]ModificationHistory, int64, error) {
	var records []ModificationHistory
	var total int64

	// Get total count
	if err := db.Model(&ModificationHistory{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Calculate offset
	offset := (page - 1) * pageSize

	// Get paginated records
	result := db.Order("date_changed DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&records)

	return records, total, result.Error
}

type ApplyBatchHistoryItem struct {
	ApplyBatch
	Modifications      []ModificationHistory  `json:"modifications"`
	IntegrationActions []IntegrationActionLog `json:"integration_actions"`
}

func CreateApplyBatch(db *gorm.DB, docCount int, summary string) (*ApplyBatch, error) {
	batch := &ApplyBatch{
		StartedAt: time.Now(),
		DocCount:  docCount,
		Summary:   summary,
	}
	if err := db.Create(batch).Error; err != nil {
		return nil, err
	}
	return batch, nil
}

func FinishApplyBatch(db *gorm.DB, batchID uint, summary string) error {
	var batch ApplyBatch
	if err := db.First(&batch, batchID).Error; err != nil {
		return err
	}
	now := time.Now()
	batch.EndedAt = &now
	if summary != "" {
		batch.Summary = summary
	}
	return db.Save(&batch).Error
}

func GetApplyBatch(db *gorm.DB, id uint) (*ApplyBatch, error) {
	var batch ApplyBatch
	if err := db.First(&batch, id).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func GetPaginatedApplyBatches(db *gorm.DB, page int, pageSize int) ([]ApplyBatchHistoryItem, int64, error) {
	var batches []ApplyBatch
	var total int64
	if err := db.Model(&ApplyBatch{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if err := db.Order("started_at DESC").Offset(offset).Limit(pageSize).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ApplyBatchHistoryItem, 0, len(batches))
	for _, batch := range batches {
		item := ApplyBatchHistoryItem{ApplyBatch: batch}
		if err := db.Where("batch_id = ?", batch.ID).Order("date_changed ASC").Find(&item.Modifications).Error; err != nil {
			return nil, 0, err
		}
		if err := db.Where("batch_id = ?", batch.ID).Order("created_at ASC").Find(&item.IntegrationActions).Error; err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, nil
}

func SetApplyBatchUndone(db *gorm.DB, batch *ApplyBatch) error {
	now := time.Now()
	batch.Undone = true
	batch.UndoneAt = &now
	return db.Save(batch).Error
}

// UndoModification marks a modification record as undone and sets the undo date
func SetModificationUndone(db *gorm.DB, record *ModificationHistory) error {
	record.Undone = true
	record.UndoneDate = time.Now().Format(time.RFC3339)
	result := db.Save(&record) // GORM's Save method
	return result.Error
}

// SaveSingleOcrPageResult saves or updates the OCR result for a single page, including GenerationInfo as JSON
func SaveSingleOcrPageResult(db *gorm.DB, docID int, pageIdx int, text string, ocrLimitHit bool, generationInfoJSON string) error {
	var result OCRPageResult
	tx := db.Where("document_id = ? AND page_index = ?", docID, pageIdx).First(&result)
	if tx.Error == nil {
		result.Text = text
		result.OcrLimitHit = ocrLimitHit
		result.GenerationInfo = generationInfoJSON
		return db.Save(&result).Error
	} else if tx.Error != nil {
		log.Debugf("SaveSingleOcrPageResult: db.First error: %v (is gorm.ErrRecordNotFound: %v)", tx.Error, errors.Is(tx.Error, gorm.ErrRecordNotFound))
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			result = OCRPageResult{
				DocumentID:     docID,
				PageIndex:      pageIdx,
				Text:           text,
				OcrLimitHit:    ocrLimitHit,
				GenerationInfo: generationInfoJSON,
			}
			return db.Create(&result).Error
		} else {
			log.Errorf("Unexpected DB error in SaveSingleOcrPageResult: %v", tx.Error)
			return tx.Error
		}
	}
	return nil
}

func GetOcrPageResults(db *gorm.DB, docID int) ([]OCRPageResult, error) {
	var results []OCRPageResult
	tx := db.Where("document_id = ?", docID).Order("page_index ASC").Find(&results)
	return results, tx.Error
}

func UpdateOcrPageResult(db *gorm.DB, docID int, pageIdx int, text string, ocrLimitHit bool, generationInfoJSON string) error {
	return SaveSingleOcrPageResult(db, docID, pageIdx, text, ocrLimitHit, generationInfoJSON)
}

func DeleteOcrPageResults(db *gorm.DB, docID int) error {
	return db.Where("document_id = ?", docID).Delete(&OCRPageResult{}).Error
}
