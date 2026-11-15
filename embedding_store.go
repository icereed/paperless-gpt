package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"paperless-gpt/sanitize"
)

const (
	embeddingPrefixRunes  = 2000
	embeddingExcerptRunes = 400
)

// DocumentEmbedding is one vector per paperless-ngx document, stored in the
// same SQLite file as the rest of paperless-gpt's local data.
type DocumentEmbedding struct {
	DocumentID    int    `gorm:"primaryKey"`
	Model         string `gorm:"index;size:255"`
	ContentHash   string `gorm:"size:64"`
	Modified      string `gorm:"size:64"`
	Vector        []byte
	Dimensions    int
	Title         string
	Correspondent string
	DocumentType  string
	Tags          string `gorm:"type:TEXT"`
	Excerpt       string `gorm:"type:TEXT"`
	UpdatedAt     time.Time
}

// similarDocument is a neighbor used to build suggestion-prompt context.
type similarDocument struct {
	ID            int
	Title         string
	Correspondent string
	DocumentType  string
	Tags          []string
	Excerpt       string
	Score         float32
}

// EmbeddingStore compares vectors in Go. One row per document keeps this
// small enough that a separate vector database is unnecessary.
type EmbeddingStore struct {
	mu    sync.Mutex
	db    *gorm.DB
	model string
}

// NewEmbeddingStore reads and writes vectors for one embedding model.
func NewEmbeddingStore(db *gorm.DB, model string) *EmbeddingStore {
	return &EmbeddingStore{db: db, model: model}
}

// Count returns how many documents are stored for this model.
func (s *EmbeddingStore) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	err := s.db.Model(&DocumentEmbedding{}).Where("model = ?", s.model).Count(&n).Error
	return int(n), err
}

// Get returns the stored row for one document and this model.
func (s *EmbeddingStore) Get(documentID int) (DocumentEmbedding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(documentID)
}

func (s *EmbeddingStore) getLocked(documentID int) (DocumentEmbedding, error) {
	var row DocumentEmbedding
	err := s.db.Where("document_id = ? AND model = ?", documentID, s.model).First(&row).Error
	return row, err
}

// Upsert inserts or replaces the row for the document in this model.
func (s *EmbeddingStore) Upsert(row DocumentEmbedding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row.Model = s.model
	row.UpdatedAt = time.Now()
	row.Dimensions = len(row.Vector) / 4
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "document_id"}},
		UpdateAll: true,
	}).Create(&row).Error
}

// UpdateMetadata refreshes the names shown in prompts without embedding again.
// A missing row is left missing: there is no vector to attach the names to.
func (s *EmbeddingStore) UpdateMetadata(doc Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tags, err := json.Marshal(removeIgnoredTags(doc.Tags))
	if err != nil {
		return err
	}
	res := s.db.Model(&DocumentEmbedding{}).
		Where("document_id = ? AND model = ?", doc.ID, s.model).
		Updates(map[string]any{
			"title":         doc.Title,
			"correspondent": doc.Correspondent,
			"document_type": doc.DocumentTypeName,
			"tags":          string(tags),
			"excerpt":       embeddingExcerpt(sanitize.Sanitize(doc.Content)),
			"modified":      doc.Modified,
			"updated_at":    time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteModel removes every row stored for this model.
func (s *EmbeddingStore) DeleteModel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Where("model = ?", s.model).Delete(&DocumentEmbedding{}).Error
}

// Delete removes one document's row for this model.
func (s *EmbeddingStore) Delete(documentID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Where("document_id = ? AND model = ?", documentID, s.model).Delete(&DocumentEmbedding{}).Error
}

// DeleteMissing removes rows for this model whose documents were not in a
// finished scan. keep is the set of paperless-ngx ids that still exist.
func (s *EmbeddingStore) DeleteMissing(keep map[int]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int
	if err := s.db.Model(&DocumentEmbedding{}).Where("model = ?", s.model).Pluck("document_id", &ids).Error; err != nil {
		return err
	}
	var drop []int
	for _, id := range ids {
		if _, ok := keep[id]; !ok {
			drop = append(drop, id)
		}
	}
	for len(drop) > 0 {
		n := len(drop)
		if n > 500 {
			n = 500
		}
		if err := s.db.Where("model = ? AND document_id IN ?", s.model, drop[:n]).Delete(&DocumentEmbedding{}).Error; err != nil {
			return err
		}
		drop = drop[n:]
	}
	return nil
}

// Similar returns the closest other documents for this embedding model.
// Vectors stored for a different model or a different length are ignored.
func (s *EmbeddingStore) Similar(query []float32, excludeID, limit int) ([]similarDocument, error) {
	if limit <= 0 || len(query) == 0 {
		return nil, nil
	}
	query = normalizeVector(query)
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows []DocumentEmbedding
	if err := s.db.Where("model = ?", s.model).Find(&rows).Error; err != nil {
		return nil, err
	}
	type scored struct {
		doc   similarDocument
		score float32
	}
	matches := make([]scored, 0, limit)
	for _, row := range rows {
		if row.DocumentID == excludeID || row.Dimensions != len(query) {
			continue
		}
		vector := decodeVector(row.Vector)
		if len(vector) != len(query) {
			continue
		}
		matches = append(matches, scored{
			score: dot(query, vector),
			doc: similarDocument{
				ID:            row.DocumentID,
				Title:         row.Title,
				Correspondent: row.Correspondent,
				DocumentType:  row.DocumentType,
				Tags:          decodeTags(row.Tags),
				Excerpt:       row.Excerpt,
			},
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].doc.ID < matches[j].doc.ID
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]similarDocument, len(matches))
	for i, match := range matches {
		match.doc.Score = match.score
		out[i] = match.doc
	}
	return out, nil
}

func embeddingPrefix(content string) string {
	return truncateRunes(content, embeddingPrefixRunes)
}

func embeddingExcerpt(content string) string {
	return truncateRunes(content, embeddingExcerptRunes)
}

func hashEmbeddingText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

func normalizeVector(vector []float32) []float32 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	if sum == 0 {
		return append([]float32(nil), vector...)
	}
	scale := float32(1 / math.Sqrt(sum))
	out := make([]float32, len(vector))
	for i, value := range vector {
		out[i] = value * scale
	}
	return out
}

func encodeVector(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}
	return buf
}

func decodeVector(buf []byte) []float32 {
	if len(buf)%4 != 0 {
		return nil
	}
	out := make([]float32, len(buf)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return out
}

func dot(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func encodeTags(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return tags
}

func embeddingRowFromDocument(doc Document, vector []float32) (DocumentEmbedding, error) {
	prefix := embeddingPrefix(doc.Content)
	if prefix == "" {
		return DocumentEmbedding{}, errors.New("document has no text to embed")
	}
	normalized := normalizeVector(vector)
	return DocumentEmbedding{
		DocumentID:    doc.ID,
		ContentHash:   hashEmbeddingText(prefix),
		Modified:      doc.Modified,
		Vector:        encodeVector(normalized),
		Dimensions:    len(normalized),
		Title:         doc.Title,
		Correspondent: doc.Correspondent,
		DocumentType:  doc.DocumentTypeName,
		Tags:          encodeTags(removeIgnoredTags(doc.Tags)),
		Excerpt:       embeddingExcerpt(doc.Content),
	}, nil
}
