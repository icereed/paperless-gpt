package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	suggestionJobStatusPending   = "pending"
	suggestionJobStatusRunning   = "running"
	suggestionJobStatusSucceeded = "succeeded"
	suggestionJobStatusFailed    = "failed"

	integrationCandidateProviderJobber = "jobber"
)

var suggestionGenerationLocks sync.Map

type suggestionCacheMetadata struct {
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	TaskModels     map[string]string `json:"task_models,omitempty"`
	PromptVersions string            `json:"prompt_versions"`
}

func envSuggestionCacheMetadata() suggestionCacheMetadata {
	return suggestionCacheMetadata{
		Provider:       strings.ToLower(strings.TrimSpace(llmProvider)),
		Model:          strings.TrimSpace(llmModel),
		PromptVersions: currentPromptVersionsHash(),
	}
}

func currentPromptVersionsHash() string {
	entries, err := os.ReadDir("prompts")
	if err != nil {
		return "prompts-unavailable"
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".tmpl") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join("prompts", name))
		if err != nil {
			continue
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(content)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func stableHash(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(fmt.Sprintf("%#v", value))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func canonicalStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	sort.Strings(result)
	return result
}

func canonicalCustomFields(fields []CustomFieldResponse) []CustomFieldResponse {
	result := append([]CustomFieldResponse(nil), fields...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Field != result[j].Field {
			return result[i].Field < result[j].Field
		}
		return fmt.Sprint(result[i].Value) < fmt.Sprint(result[j].Value)
	})
	return result
}

func canonicalIntSlice(values []int) []int {
	result := append([]int(nil), values...)
	sort.Ints(result)
	return result
}

func suggestionSourceHash(doc Document, req GenerateSuggestionsRequest, metadata suggestionCacheMetadata) string {
	settingsMutex.RLock()
	cacheSettings := map[string]interface{}{
		"custom_fields_enable":                settings.CustomFieldsEnable,
		"custom_fields_selected_ids":          canonicalIntSlice(settings.CustomFieldsSelectedIDs),
		"custom_fields_write_mode":            settings.CustomFieldsWriteMode,
		"restrict_tags_to_existing":           settings.RestrictTagsToExisting,
		"restrict_correspondents_to_existing": settings.RestrictCorrespondentsToExisting,
		"restrict_document_types_to_existing": settings.RestrictDocumentTypesToExisting,
		"create_new_tags":                     createNewTags,
		"correspondent_black_list":            canonicalStringSlice(correspondentBlackList),
	}
	settingsMutex.RUnlock()

	return stableHash(map[string]interface{}{
		"document": map[string]interface{}{
			"id":                 doc.ID,
			"title":              doc.Title,
			"content":            doc.Content,
			"tags":               canonicalStringSlice(doc.Tags),
			"correspondent":      doc.Correspondent,
			"created_date":       doc.CreatedDate,
			"document_type_name": doc.DocumentTypeName,
			"custom_fields":      canonicalCustomFields(doc.CustomFields),
		},
		"request": map[string]bool{
			"generate_titles":         req.GenerateTitles,
			"generate_tags":           req.GenerateTags,
			"generate_correspondents": req.GenerateCorrespondents,
			"generate_created_date":   req.GenerateCreatedDate,
			"generate_custom_fields":  req.GenerateCustomFields,
			"generate_document_types": req.GenerateDocumentTypes,
		},
		"settings": cacheSettings,
		"metadata": metadata,
	})
}

func (app *App) getCachedSuggestion(ctx context.Context, doc Document, req GenerateSuggestionsRequest, metadata suggestionCacheMetadata) (*DocumentSuggestion, *DocumentSuggestionCache, error) {
	sourceHash := suggestionSourceHash(doc, req, metadata)
	var cache DocumentSuggestionCache
	err := app.Database.WithContext(ctx).
		Where("document_id = ? AND source_hash = ?", doc.ID, sourceHash).
		First(&cache).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var suggestion DocumentSuggestion
	if err := json.Unmarshal([]byte(cache.SuggestionsJSON), &suggestion); err != nil {
		return nil, nil, err
	}
	suggestion.Cached = true
	suggestion.GeneratedAt = cache.GeneratedAt.Format(time.RFC3339)
	return &suggestion, &cache, nil
}

func (app *App) saveSuggestionCache(ctx context.Context, suggestion DocumentSuggestion, req GenerateSuggestionsRequest, metadata suggestionCacheMetadata) error {
	sourceHash := suggestionSourceHash(suggestion.OriginalDocument, req, metadata)
	payloadSuggestion := suggestion
	payloadSuggestion.Cached = false
	payloadSuggestion.GeneratedAt = ""
	suggestionJSON, err := json.Marshal(payloadSuggestion)
	if err != nil {
		return err
	}
	requestJSON, _ := json.Marshal(req)
	now := time.Now()

	var existing DocumentSuggestionCache
	db := app.Database.WithContext(ctx)
	err = db.Where("document_id = ? AND source_hash = ?", suggestion.ID, sourceHash).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		existing = DocumentSuggestionCache{
			DocumentID: suggestion.ID,
			SourceHash: sourceHash,
		}
	}
	existing.GeneratedAt = now
	existing.SuggestionsJSON = string(suggestionJSON)
	existing.Provider = metadata.Provider
	existing.Model = metadata.Model
	existing.PromptVersions = metadata.PromptVersions
	existing.GenerationRequestJSON = string(requestJSON)
	if existing.ID == 0 {
		return db.Create(&existing).Error
	}
	return db.Save(&existing).Error
}

func (app *App) generateDocumentSuggestionsCached(ctx context.Context, req GenerateSuggestionsRequest, logger *logrus.Entry) ([]DocumentSuggestion, error) {
	metadata := app.currentSuggestionCacheMetadata(ctx)
	resultsByID := make(map[int]DocumentSuggestion, len(req.Documents))
	misses := make([]Document, 0)

	for _, doc := range req.Documents {
		if !req.Regenerate {
			cached, _, err := app.getCachedSuggestion(ctx, doc, req, metadata)
			if err != nil {
				logger.WithError(err).WithField("document_id", doc.ID).Warn("Failed to read suggestion cache; regenerating")
			} else if cached != nil {
				resultsByID[doc.ID] = *cached
				continue
			}
		}
		misses = append(misses, doc)
	}

	if len(misses) > 0 {
		locked, unlock := acquireSuggestionGenerationLocks(misses, req, metadata)
		defer unlock()
		misses = locked
		if len(misses) == 0 {
			return app.generateDocumentSuggestionsCached(ctx, req, logger)
		}
		missReq := req
		missReq.Documents = misses
		missReq.Regenerate = false
		freshSuggestions, err := app.generateDocumentSuggestions(ctx, missReq, logger)
		if err != nil {
			return nil, err
		}
		for _, suggestion := range freshSuggestions {
			if err := app.saveSuggestionCache(ctx, suggestion, req, metadata); err != nil {
				logger.WithError(err).WithField("document_id", suggestion.ID).Warn("Failed to save suggestion cache")
			}
			suggestion.Cached = false
			suggestion.GeneratedAt = time.Now().Format(time.RFC3339)
			resultsByID[suggestion.ID] = suggestion
		}
	}

	ordered := make([]DocumentSuggestion, 0, len(req.Documents))
	for _, doc := range req.Documents {
		if suggestion, ok := resultsByID[doc.ID]; ok {
			ordered = append(ordered, suggestion)
		}
	}
	return ordered, nil
}

func (app *App) invalidateDocumentSuggestionCache(ctx context.Context, documentID int) error {
	return app.Database.WithContext(ctx).Where("document_id = ?", documentID).Delete(&DocumentSuggestionCache{}).Error
}

func (app *App) enqueueSuggestionJob(ctx context.Context, documentID int, sourceHash string) error {
	db := app.Database.WithContext(ctx)
	var existing SuggestionJob
	query := db.Where("document_id = ? AND status IN ?", documentID, []string{suggestionJobStatusPending, suggestionJobStatusFailed, suggestionJobStatusRunning})
	if strings.TrimSpace(sourceHash) != "" {
		query = query.Where("source_hash = ?", sourceHash)
	}
	err := query.Order("created_at DESC").First(&existing).Error
	if err == nil {
		if existing.Status != suggestionJobStatusRunning {
			existing.Status = suggestionJobStatusPending
			existing.NextAttemptAt = time.Now()
			existing.LastError = ""
			if strings.TrimSpace(sourceHash) != "" {
				existing.SourceHash = sourceHash
			}
			return db.Save(&existing).Error
		}
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	job := SuggestionJob{
		DocumentID:    documentID,
		SourceHash:    sourceHash,
		Status:        suggestionJobStatusPending,
		NextAttemptAt: time.Now(),
	}
	return db.Create(&job).Error
}

func acquireSuggestionGenerationLocks(documents []Document, req GenerateSuggestionsRequest, metadata suggestionCacheMetadata) ([]Document, func()) {
	type acquiredLock struct {
		key string
		ch  chan struct{}
	}
	locked := make([]Document, 0, len(documents))
	acquired := make([]acquiredLock, 0, len(documents))
	for _, doc := range documents {
		key := suggestionSourceHash(doc, req, metadata)
		ch := make(chan struct{})
		actual, loaded := suggestionGenerationLocks.LoadOrStore(key, ch)
		if loaded {
			if existing, ok := actual.(chan struct{}); ok {
				<-existing
			}
			continue
		}
		locked = append(locked, doc)
		acquired = append(acquired, acquiredLock{key: key, ch: ch})
	}
	return locked, func() {
		for _, lock := range acquired {
			if actual, ok := suggestionGenerationLocks.Load(lock.key); ok && actual == lock.ch {
				suggestionGenerationLocks.Delete(lock.key)
				close(lock.ch)
			}
		}
	}
}

func integrationMatchHash(provider string, value interface{}) string {
	return stableHash(map[string]interface{}{
		"provider": provider,
		"input":    value,
	})
}

func (app *App) getCachedIntegrationCandidates(ctx context.Context, documentID int, provider, matchHash string, target interface{}) (string, bool, error) {
	var cache IntegrationCandidateCache
	err := app.Database.WithContext(ctx).
		Where("document_id = ? AND provider = ? AND match_hash = ?", documentID, provider, matchHash).
		First(&cache).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	if err := json.Unmarshal([]byte(cache.CandidatesJSON), target); err != nil {
		return "", false, err
	}
	return cache.AutoSelectedID, true, nil
}

func (app *App) saveIntegrationCandidates(ctx context.Context, documentID int, provider, matchHash string, candidates interface{}, autoSelectedID string) error {
	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return err
	}
	db := app.Database.WithContext(ctx)
	var cache IntegrationCandidateCache
	err = db.Where("document_id = ? AND provider = ? AND match_hash = ?", documentID, provider, matchHash).First(&cache).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if err == gorm.ErrRecordNotFound {
		cache = IntegrationCandidateCache{
			DocumentID: documentID,
			Provider:   provider,
			MatchHash:  matchHash,
		}
	}
	cache.CandidatesJSON = string(candidatesJSON)
	cache.AutoSelectedID = autoSelectedID
	cache.GeneratedAt = time.Now()
	if cache.ID == 0 {
		return db.Create(&cache).Error
	}
	return db.Save(&cache).Error
}
