package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func openEmbeddingDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&DocumentEmbedding{}))
	return db
}

func TestEmbeddingStoreSimilar(t *testing.T) {
	db := openEmbeddingDB(t)
	store := NewEmbeddingStore(db, "nomic")

	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 1, Title: "Self", Content: "self", Tags: []string{"invoice"},
	}, []float32{1, 0, 0})))
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 2, Title: "Close", Content: "close", Correspondent: "Stadtwerke", DocumentTypeName: "Invoice", Tags: []string{"utilities", "invoice"},
	}, []float32{0.9, 0.1, 0})))
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 3, Title: "Far", Content: "far", Tags: []string{"letter"},
	}, []float32{0, 1, 0})))
	// A different model shares the table but must not be a neighbor.
	other := NewEmbeddingStore(db, "other")
	require.NoError(t, other.Upsert(mustRow(t, Document{
		ID: 4, Title: "Other model", Content: "other",
	}, []float32{1, 0, 0})))
	// A vector of a different length is ignored.
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 5, Title: "Wrong size", Content: "size",
	}, []float32{1, 0})))

	// Upserting the same document again replaces the row instead of inserting a second one.
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 2, Title: "Closer", Content: "close", Correspondent: "Stadtwerke", DocumentTypeName: "Invoice", Tags: []string{"utilities", "invoice"},
	}, []float32{0.9, 0.1, 0})))

	matches, err := store.Similar([]float32{1, 0, 0}, 1, 5)
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, 2, matches[0].ID)
	assert.Equal(t, "Closer", matches[0].Title)
	assert.Equal(t, "Stadtwerke", matches[0].Correspondent)
	assert.Equal(t, []string{"utilities", "invoice"}, matches[0].Tags)
	assert.Equal(t, 3, matches[1].ID)

	count, err := store.Count()
	require.NoError(t, err)
	assert.Equal(t, 4, count) // ids 1, 2, 3, 5 — not the other model
	otherCount, err := other.Count()
	require.NoError(t, err)
	assert.Equal(t, 1, otherCount)
}

func TestEmbeddingStoreDeleteMissing(t *testing.T) {
	db := openEmbeddingDB(t)
	store := NewEmbeddingStore(db, "nomic")
	require.NoError(t, store.Upsert(mustRow(t, Document{ID: 1, Content: "a"}, []float32{1, 0})))
	require.NoError(t, store.Upsert(mustRow(t, Document{ID: 2, Content: "b"}, []float32{0, 1})))

	require.NoError(t, store.DeleteMissing(map[int]struct{}{1: {}}))
	_, err := store.Get(2)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = store.Get(1)
	assert.NoError(t, err)
}

func TestUpdateMetadataLeavesVector(t *testing.T) {
	db := openEmbeddingDB(t)
	store := NewEmbeddingStore(db, "nomic")
	require.NoError(t, store.Upsert(mustRow(t, Document{ID: 8, Title: "Scan", Content: "invoice body", Tags: []string{"old"}}, []float32{1, 0})))
	before, err := store.Get(8)
	require.NoError(t, err)

	err = store.UpdateMetadata(Document{
		ID: 8, Title: "Electric bill", Content: "invoice body", Correspondent: "Stadtwerke", DocumentTypeName: "Invoice", Tags: []string{"utilities"}, Modified: "2024-05-01T00:00:00Z",
	})
	require.NoError(t, err)
	after, err := store.Get(8)
	require.NoError(t, err)
	assert.Equal(t, before.Vector, after.Vector)
	assert.Equal(t, before.ContentHash, after.ContentHash)
	assert.Equal(t, "Electric bill", after.Title)
	assert.Equal(t, "Stadtwerke", after.Correspondent)
	assert.Equal(t, "Invoice", after.DocumentType)
	assert.Equal(t, []string{"utilities"}, decodeTags(after.Tags))
	assert.Equal(t, "2024-05-01T00:00:00Z", after.Modified)

	err = store.UpdateMetadata(Document{ID: 99, Content: "missing"})
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestIgnoredTagsAreNotStoredWithEmbeddings(t *testing.T) {
	withIgnoredTags(t, []string{"Inbox"})
	db := openEmbeddingDB(t)
	store := NewEmbeddingStore(db, "nomic")
	require.NoError(t, store.Upsert(mustRow(t, Document{
		ID: 4, Content: "invoice body", Tags: []string{"Inbox", "utilities"},
	}, []float32{1, 0})))
	row, err := store.Get(4)
	require.NoError(t, err)
	assert.Equal(t, []string{"utilities"}, decodeTags(row.Tags))

	require.NoError(t, store.UpdateMetadata(Document{
		ID: 4, Content: "invoice body", Tags: []string{"inbox", "taxes"},
	}))
	row, err = store.Get(4)
	require.NoError(t, err)
	assert.Equal(t, []string{"taxes"}, decodeTags(row.Tags))
}

func mustRow(t *testing.T, doc Document, vector []float32) DocumentEmbedding {
	t.Helper()
	row, err := embeddingRowFromDocument(doc, vector)
	require.NoError(t, err)
	return row
}
