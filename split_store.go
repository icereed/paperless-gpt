package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"gorm.io/gorm"
)

// SplitSuggestion records an entire batch decision, including uncertain submissions.
type SplitSuggestion struct {
	ID                uint            `gorm:"primaryKey" json:"id"`
	DocumentID        int             `gorm:"uniqueIndex;not null" json:"document_id"`
	DocumentTitle     string          `json:"document_title"`
	PageCount         int             `json:"page_count"`
	Status            string          `gorm:"index;not null" json:"status"`
	Boundaries        []SplitBoundary `gorm:"serializer:json;type:text" json:"boundaries"`
	AppliedBoundaries []int           `gorm:"serializer:json;type:text" json:"applied_boundaries,omitempty"`
	SourceChecksum    string          `json:"-"`
	ArchiveChecksum   string          `json:"-"`
	OriginalTags      []int           `gorm:"serializer:json;type:text" json:"-"`
	OutputTagIDs      []int           `gorm:"serializer:json;type:text" json:"-"`
	SplitTagID        int             `json:"-"`
	Error             string          `json:"error,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

func markInterruptedSplits(db *gorm.DB) error {
	if err := db.Model(&SplitSuggestion{}).Where("status = ?", "generating").Updates(map[string]interface{}{"status": "failed", "error": "Generation interrupted; generate again to retry"}).Error; err != nil {
		return err
	}
	return db.Model(&SplitSuggestion{}).Where("status = ?", "applying").Updates(map[string]interface{}{"status": "uncertain", "error": "Apply interrupted; inspect paperless-ngx before any further split"}).Error
}

func (app *App) splitHeld(ctx context.Context, id int, tags []string) (bool, error) {
	if app.splitConfig == nil {
		return false, nil
	}
	if hasSplitTag(tags, app.splitConfig.Tag) {
		return true, nil
	}
	var count int64
	err := app.Database.WithContext(ctx).Model(&SplitSuggestion{}).Where("document_id = ? AND status NOT IN ?", id, []string{"failed", "kept"}).Count(&count).Error
	return count != 0, err
}

func (app *App) processSplitTagDocuments(ctx context.Context) (int, error) {
	if app == nil || app.splitConfig == nil {
		return 0, nil
	}
	client, ok := app.Client.(splitClient)
	if !ok {
		return 0, fmt.Errorf("split API client unavailable")
	}
	ids, err := client.ListSplitDocumentIDs(ctx, app.splitConfig.Tag)
	if err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		created, err := app.processSplitDocument(ctx, id)
		if created {
			count++
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("document %d: %w", id, err))
		}
	}
	return count, errors.Join(failures...)
}

func (app *App) processSplitDocument(ctx context.Context, id int) (bool, error) {
	app.splitMu.Lock()
	defer app.splitMu.Unlock()
	var existing int64
	if err := app.Database.WithContext(ctx).Model(&SplitSuggestion{}).Where("document_id = ?", id).Count(&existing).Error; err != nil {
		return false, err
	}
	if existing != 0 {
		return false, nil
	}
	suggestion, err := app.generateSplitSuggestion(ctx, id, false)
	if err != nil {
		return false, err
	}
	if boundaries := automaticSplitBoundaries(app.splitConfig, suggestion); len(boundaries) > 0 {
		if _, err := app.applySplitSuggestion(ctx, suggestion.ID, boundaries); err != nil {
			saveErr := app.Database.WithContext(ctx).Model(&SplitSuggestion{}).Where("id = ?", suggestion.ID).Update("error", err.Error()).Error
			return true, fmt.Errorf("automatic Apply: %w", errors.Join(err, saveErr))
		}
	}
	return true, nil
}

// Callers hold splitMu, shared with OCR and metadata processing through Apply cleanup.
func (app *App) generateSplitSuggestion(ctx context.Context, id int, regenerate bool) (*SplitSuggestion, error) {
	client, ok := app.Client.(splitClient)
	if !ok {
		return nil, fmt.Errorf("split API client unavailable")
	}
	var suggestion SplitSuggestion
	err := app.Database.WithContext(ctx).Where("document_id = ?", id).First(&suggestion).Error
	if err == nil {
		if !regenerate {
			return &suggestion, nil
		}
		if suggestion.Status != "review" && suggestion.Status != "failed" && suggestion.Status != "skipped" {
			return nil, fmt.Errorf("cannot regenerate a %s split", suggestion.Status)
		}
		suggestion.Status, suggestion.Error = "generating", ""
		if err := app.Database.WithContext(ctx).Save(&suggestion).Error; err != nil {
			return nil, err
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		suggestion = SplitSuggestion{DocumentID: id, Status: "generating", Boundaries: []SplitBoundary{}}
		if err := app.Database.WithContext(ctx).Create(&suggestion).Error; err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	fail := func(cause error) (*SplitSuggestion, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		suggestion.Status, suggestion.Error = "failed", cause.Error()
		saveErr := app.Database.WithContext(cleanupCtx).Save(&suggestion).Error
		tagErr := app.tagSplitFailure(cleanupCtx, client, id)
		return &suggestion, errors.Join(cause, saveErr, tagErr)
	}
	source, err := client.GetSplitSource(ctx, id)
	if err != nil {
		return fail(err)
	}
	data, err := client.DownloadPDF(ctx, Document{ID: id})
	if err != nil {
		return fail(err)
	}
	pages, err := extractSplitPages(ctx, data)
	if err != nil {
		return fail(err)
	}
	if len(pages) != source.PageCount {
		return fail(fmt.Errorf("archive and source page counts differ"))
	}
	latest, err := client.GetSplitSource(ctx, id)
	if err != nil {
		return fail(err)
	}
	if latest.Checksum != source.Checksum || latest.PageCount != source.PageCount {
		return fail(fmt.Errorf("source changed during split analysis"))
	}
	if err := client.InvalidateSplitPreviews(id); err != nil {
		return fail(fmt.Errorf("cannot refresh page previews: %w", err))
	}
	suggestion.DocumentTitle, suggestion.PageCount = source.Title, len(pages)
	suggestion.SourceChecksum = source.Checksum
	suggestion.ArchiveChecksum = fmt.Sprintf("%x", sha256.Sum256(data))
	templateMutex.RLock()
	tmpl := splitTemplate
	templateMutex.RUnlock()
	results := []splitWindowResult{}
	for _, window := range splitWindows(len(pages), app.splitConfig.WindowSize) {
		prompt, err := renderSplitPrompt(tmpl, pages[window.First-1:window.Last], len(pages))
		if err != nil {
			return fail(err)
		}
		var boundaries []SplitBoundary
		for attempt := 0; attempt < 2; attempt++ {
			response, callErr := app.LLM.Call(ctx, prompt)
			if callErr != nil {
				return fail(callErr)
			}
			boundaries, err = parseSplitBoundaries(response, window.First, window.Last)
			if err == nil {
				break
			}
			prompt += "\nYour previous response failed validation: " + err.Error() + ". Return only the required JSON array."
		}
		if err != nil {
			return fail(err)
		}
		results = append(results, splitWindowResult{window, boundaries})
	}
	suggestion.Boundaries = mergeSplitWindows(results)
	applySplitRules(pages, suggestion.Boundaries)
	suggestion.Status, suggestion.Error = "review", ""
	if err := app.Database.WithContext(ctx).Save(&suggestion).Error; err != nil {
		return fail(err)
	}
	// Reasons may quote private document text; log only positions and confidence.
	for _, b := range suggestion.Boundaries {
		documentLogger(id).Infof("Split suggestion after page %d: confidence=%.3f conflict=%t blocked=%t dry_run=%t", b.AfterPage, b.Confidence, b.Conflict, b.BlockedReason != "", app.splitConfig.DryRun)
	}
	return &suggestion, nil
}

func (app *App) tagSplitFailure(ctx context.Context, client splitClient, id int) error {
	if err := client.EnsureTagExists(ctx, failTag); err != nil {
		return err
	}
	tags, err := app.Client.GetAllTags(ctx)
	if err != nil {
		return err
	}
	_, failID, ok := lookupTagID(tags, failTag)
	if !ok {
		return fmt.Errorf("FAIL_TAG unavailable")
	}
	// GetDocument also works for unsupported source formats and missing page_count.
	doc, err := app.Client.GetDocument(ctx, id)
	if err != nil {
		return err
	}
	if !slices.Contains(doc.TagIDs, failID) {
		doc.TagIDs = append(doc.TagIDs, failID)
	}
	return client.SetSplitTags(ctx, id, doc.TagIDs)
}

func (app *App) applySplitSuggestion(ctx context.Context, id uint, boundaries []int) (*SplitSuggestion, error) {
	if app.splitConfig.DryRun {
		return nil, fmt.Errorf("SPLIT_DRY_RUN prevents Apply")
	}
	var s SplitSuggestion
	if err := app.Database.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, err
	}
	if s.Status != "review" && s.Status != "skipped" {
		return nil, fmt.Errorf("cannot apply a %s split", s.Status)
	}
	if len(boundaries) > 0 {
		if _, err := splitPagesString(s.PageCount, boundaries); err != nil {
			return nil, err
		}
	}
	client, ok := app.Client.(splitClient)
	if !ok {
		return nil, fmt.Errorf("split API client unavailable")
	}
	source, err := client.GetSplitSource(ctx, s.DocumentID)
	if err != nil {
		return nil, err
	}
	data, err := client.DownloadPDF(ctx, Document{ID: s.DocumentID})
	if err != nil {
		return nil, err
	}
	if source.Checksum != s.SourceChecksum || source.PageCount != s.PageCount || fmt.Sprintf("%x", sha256.Sum256(data)) != s.ArchiveChecksum {
		return nil, fmt.Errorf("source changed; generate a new split suggestion")
	}
	for _, tag := range append([]string{app.splitConfig.Tag}, app.splitConfig.OutputTags...) {
		if err := client.EnsureTagExists(ctx, tag); err != nil {
			return nil, err
		}
	}
	tags, err := app.Client.GetAllTags(ctx)
	if err != nil {
		return nil, err
	}
	_, s.SplitTagID, ok = lookupTagID(tags, app.splitConfig.Tag)
	if !ok {
		return nil, fmt.Errorf("SPLIT_TAG unavailable")
	}
	latest, err := client.GetSplitSource(ctx, s.DocumentID)
	if err != nil {
		return nil, err
	}
	if latest.Checksum != s.SourceChecksum || latest.PageCount != s.PageCount {
		return nil, fmt.Errorf("source changed; generate a new split suggestion")
	}
	s.OriginalTags = slices.Clone(latest.Tags)
	s.OutputTagIDs = nil
	swapped := slices.DeleteFunc(slices.Clone(latest.Tags), func(id int) bool { return id == s.SplitTagID })
	for _, name := range app.splitConfig.OutputTags {
		_, tagID, found := lookupTagID(tags, name)
		if !found {
			return nil, fmt.Errorf("split output tag %q unavailable", name)
		}
		if !slices.Contains(swapped, tagID) {
			swapped = append(swapped, tagID)
			s.OutputTagIDs = append(s.OutputTagIDs, tagID)
		}
	}
	s.Status, s.Error, s.AppliedBoundaries = "applying", "", slices.Clone(boundaries)
	result := app.Database.WithContext(ctx).Model(&SplitSuggestion{}).Where("id = ? AND status IN ?", s.ID, []string{"review", "skipped"}).Updates(map[string]interface{}{
		"status": s.Status, "error": "", "original_tags": splitJSON(s.OriginalTags), "output_tag_ids": splitJSON(s.OutputTagIDs), "split_tag_id": s.SplitTagID, "applied_boundaries": splitJSON(boundaries),
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("split already claimed")
	}
	swapErr := client.SetSplitTags(ctx, s.DocumentID, swapped)
	if len(boundaries) == 0 {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		s.Status = "kept"
		if swapErr != nil {
			s.Status, s.Error = "review", swapErr.Error()
		}
		saveErr := app.Database.WithContext(cleanupCtx).Save(&s).Error
		return &s, errors.Join(swapErr, saveErr)
	}
	var submitErr error
	if swapErr == nil {
		submitErr = client.SplitDocument(ctx, s.DocumentID, s.PageCount, boundaries, app.splitConfig.DeleteOriginals)
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	// Even delete_originals queues deletion later; remove temporary tags while the original exists.
	cleanupErr := app.restoreSplitTags(cleanupCtx, client, &s, swapErr != nil)
	s.Status = "submitted"
	if swapErr != nil {
		s.Status = "review"
	}
	if submitErr != nil || cleanupErr != nil {
		s.Status = "uncertain"
	}
	combined := errors.Join(swapErr, submitErr, cleanupErr)
	if combined != nil {
		s.Error = combined.Error()
	}
	saveErr := app.Database.WithContext(cleanupCtx).Save(&s).Error
	return &s, errors.Join(combined, saveErr)
}

func splitJSON(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

func (app *App) restoreSplitTags(ctx context.Context, client splitClient, s *SplitSuggestion, restoreTrigger bool) error {
	// Read current tags so unrelated edits made during the split are not overwritten.
	doc, err := client.GetSplitSource(ctx, s.DocumentID)
	if err != nil {
		var status *splitHTTPError
		if app.splitConfig.DeleteOriginals && errors.As(err, &status) && status.Status == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("cannot restore original tags: %w", err)
	}
	tags := slices.DeleteFunc(slices.Clone(doc.Tags), func(id int) bool { return id == s.SplitTagID || slices.Contains(s.OutputTagIDs, id) })
	if restoreTrigger && slices.Contains(s.OriginalTags, s.SplitTagID) {
		tags = append(tags, s.SplitTagID)
	}
	return client.SetSplitTags(ctx, s.DocumentID, tags)
}

// Exclude pending split batches before the server applies the 25-document poll limit.
func (app *App) processingDocuments(ctx context.Context, tag string) ([]Document, error) {
	if app.splitConfig != nil {
		if client, ok := app.Client.(interface {
			GetDocumentsByTagExcluding(context.Context, string, int, string, int) ([]Document, error)
		}); ok {
			for page := 1; ; page++ {
				documents, err := client.GetDocumentsByTagExcluding(ctx, tag, 25, app.splitConfig.Tag, page)
				if err != nil {
					return nil, err
				}
				eligible := make([]Document, 0, len(documents))
				for _, document := range documents {
					held, err := app.splitHeld(ctx, document.ID, document.Tags)
					if err != nil {
						return nil, err
					}
					if !held {
						eligible = append(eligible, document)
					}
				}
				if len(eligible) > 0 || len(documents) < 25 {
					return eligible, nil
				}
			}
		}
	}
	return app.Client.GetDocumentsByTag(ctx, tag, 25)
}
