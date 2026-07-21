package main

import (
	"time"

	"gorm.io/gorm"
)

// OCRRun is the persisted record of one OCR execution for one document.
// It survives restarts so the Activity view can answer "what did OCR do,
// when, with which options, and did it touch any originals?" — and so any
// run can be repeated with adjusted options.
type OCRRun struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	JobID         string `gorm:"index;size:64" json:"job_id"`
	DocumentID    int    `gorm:"index;not null" json:"document_id"`
	DocumentTitle string `gorm:"size:1024" json:"document_title"`
	Trigger       string `gorm:"size:16;not null" json:"trigger"` // "manual" | "auto"
	Status        string `gorm:"size:16;not null" json:"status"`  // "in_progress", "completed", "failed", "cancelled", "interrupted"

	// Run options (flattened for querying and re-running)
	LimitPages       int    `json:"limit_pages"`
	ProcessMode      string `gorm:"size:16" json:"process_mode"`
	UploadPDF        bool   `json:"upload_pdf"`
	ReplaceOriginal  bool   `json:"replace_original"`
	CopyMetadata     bool   `json:"copy_metadata"`
	PromptOverridden bool   `json:"prompt_overridden"`
	PromptOverride   string `gorm:"type:TEXT" json:"prompt_override,omitempty"`

	Provider string `gorm:"size:128" json:"provider"`

	PagesDone  int `json:"pages_done"`
	TotalPages int `json:"total_pages"`

	// What happened to the searchable PDF: "none", "attached", "replaced",
	// "skipped" (e.g. page limit below document length), or "failed".
	PDFAction string `gorm:"size:16" json:"pdf_action"`
	PDFDetail string `gorm:"size:1024" json:"pdf_detail,omitempty"`

	Error string `gorm:"type:TEXT" json:"error,omitempty"`

	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"-"`
	UpdatedAt  time.Time  `json:"-"`
}

const (
	// Page texts are kept for this many most-recent runs per document so the
	// Playground can compare runs without the database growing unbounded.
	ocrRunsWithPagesPerDocument = 5
	// Hard cap on stored run records (small rows; pruned oldest-first).
	maxOCRRunRecords = 1000
)

// CreateOCRRun inserts the record for a starting run.
func CreateOCRRun(db *gorm.DB, run *OCRRun) error {
	run.Status = "in_progress"
	run.StartedAt = time.Now()
	return db.Create(run).Error
}

// FinishOCRRun marks a run as finished with its outcome.
func FinishOCRRun(db *gorm.DB, jobID string, status string, errorMsg string, pagesDone, totalPages int, pdfAction, pdfDetail string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":      status,
		"error":       errorMsg,
		"pages_done":  pagesDone,
		"total_pages": totalPages,
		"finished_at": &now,
	}
	if pdfAction != "" {
		updates["pdf_action"] = pdfAction
		updates["pdf_detail"] = pdfDetail
	}
	return db.Model(&OCRRun{}).Where("job_id = ?", jobID).Updates(updates).Error
}

// MarkInterruptedOCRRuns flags runs that were in progress when the process
// died. Called once on startup.
func MarkInterruptedOCRRuns(db *gorm.DB) error {
	return db.Model(&OCRRun{}).Where("status = ?", "in_progress").
		Updates(map[string]interface{}{"status": "interrupted", "error": "paperless-gpt restarted while this run was in progress"}).Error
}

// ListOCRRuns returns runs newest-first, optionally filtered by document.
func ListOCRRuns(db *gorm.DB, documentID int, limit, offset int) ([]OCRRun, int64, error) {
	query := db.Model(&OCRRun{})
	if documentID > 0 {
		query = query.Where("document_id = ?", documentID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var runs []OCRRun
	err := query.Order("started_at DESC").Limit(limit).Offset(offset).Find(&runs).Error
	return runs, total, err
}

// GetOCRRunByJobID returns a single run.
func GetOCRRunByJobID(db *gorm.DB, jobID string) (*OCRRun, error) {
	var run OCRRun
	err := db.Where("job_id = ?", jobID).First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// PruneOCRRuns keeps page texts for the newest runs of a document and caps
// the total number of run records. Best-effort; errors are returned for
// logging but never abort a run.
func PruneOCRRuns(db *gorm.DB, documentID int) error {
	// Collect the job IDs of the runs whose page texts we keep.
	var keepJobIDs []string
	if err := db.Model(&OCRRun{}).
		Where("document_id = ? AND job_id <> ''", documentID).
		Order("started_at DESC").
		Limit(ocrRunsWithPagesPerDocument).
		Pluck("job_id", &keepJobIDs).Error; err != nil {
		return err
	}
	// Delete page results of this document that belong to older runs.
	// Legacy rows (job_id = '') are also dropped once newer runs exist.
	if len(keepJobIDs) > 0 {
		if err := db.Where("document_id = ? AND job_id NOT IN ?", documentID, keepJobIDs).
			Delete(&OCRPageResult{}).Error; err != nil {
			return err
		}
	}
	// Cap total run records, oldest first.
	var total int64
	if err := db.Model(&OCRRun{}).Count(&total).Error; err != nil {
		return err
	}
	if total > maxOCRRunRecords {
		type oldestRun struct {
			ID    uint
			JobID string
		}
		var oldest []oldestRun
		if err := db.Model(&OCRRun{}).Order("started_at ASC").
			Limit(int(total - maxOCRRunRecords)).
			Find(&oldest).Error; err != nil {
			return err
		}
		if len(oldest) > 0 {
			ids := make([]uint, 0, len(oldest))
			jobIDs := make([]string, 0, len(oldest))
			for _, r := range oldest {
				ids = append(ids, r.ID)
				if r.JobID != "" {
					jobIDs = append(jobIDs, r.JobID)
				}
			}
			// Delete the runs' page results first so they are never orphaned
			// (left behind with no matching OCRRun) by the record cap.
			if len(jobIDs) > 0 {
				if err := db.Where("job_id IN ?", jobIDs).Delete(&OCRPageResult{}).Error; err != nil {
					return err
				}
			}
			if err := db.Where("id IN ?", ids).Delete(&OCRRun{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// LatestOCRRunJobID returns the job id of the newest run of a document that
// has stored page results, or "" if none exist.
func LatestOCRRunJobID(db *gorm.DB, documentID int) string {
	var jobID string
	db.Model(&OCRPageResult{}).
		Where("document_id = ?", documentID).
		Order("updated_at DESC").
		Limit(1).
		Pluck("job_id", &jobID)
	return jobID
}
