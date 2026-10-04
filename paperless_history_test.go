package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateDocuments_HistoryStoresHumanReadableNames is a regression test for
// https://github.com/icereed/paperless-gpt/issues/842
// History showed Previous as names but New as IDs, e.g.
// Previous: [paperless-gpt-auto paperless-gpt-failed] / New: [3 2].
func TestUpdateDocuments_HistoryStoresHumanReadableNames(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"id":2,"name":"tag2"},{"id":3,"name":"keepMe"}],"next":null}`))
	})
	env.setMockResponse("/api/correspondents/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"id":1,"name":"Alpha"},{"id":2,"name":"Beta"}]}`))
	})
	env.setMockResponse("/api/document_types/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[{"id":9,"name":"Invoice"}]}`))
	})
	env.setMockResponse("/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	doc := DocumentSuggestion{
		ID: 1,
		OriginalDocument: Document{
			ID:               1,
			Title:            "Old",
			Tags:             []string{"tag1"},
			Correspondent:    "Alpha",
			DocumentTypeName: "",
		},
		SuggestedTags:          []string{"tag2", "keepMe"},
		SuggestedCorrespondent: "Beta",
		SuggestedDocumentType:  "Invoice",
	}

	require.NoError(t, env.client.UpdateDocuments(context.Background(), []DocumentSuggestion{doc}, env.db, false))

	var mods []ModificationHistory
	require.NoError(t, env.db.Find(&mods).Error)
	byField := map[string]ModificationHistory{}
	for _, m := range mods {
		byField[m.ModField] = m
	}

	// Tags must be JSON names on both sides, not IDs.
	tagsMod, ok := byField["tags"]
	require.True(t, ok, "expected tags modification, got %v", byField)
	var prevTags, newTags []string
	require.NoError(t, json.Unmarshal([]byte(tagsMod.PreviousValue), &prevTags))
	require.NoError(t, json.Unmarshal([]byte(tagsMod.NewValue), &newTags))
	assert.Equal(t, []string{"tag1"}, prevTags)
	assert.ElementsMatch(t, []string{"tag2", "keepMe"}, newTags)

	// Correspondent / document type must be names, not IDs.
	corrMod, ok := byField["correspondent"]
	require.True(t, ok, "expected correspondent modification")
	assert.Equal(t, "Alpha", corrMod.PreviousValue)
	assert.Equal(t, "Beta", corrMod.NewValue)

	typeMod, ok := byField["document_type"]
	require.True(t, ok, "expected document_type modification")
	assert.Equal(t, "", typeMod.PreviousValue)
	assert.Equal(t, "Invoice", typeMod.NewValue)
}

func TestParseTagHistoryValue_BackwardCompat(t *testing.T) {
	known := map[string]int{
		"paperless-gpt-auto":   1,
		"paperless-gpt-failed": 2,
		"Boîte de réception":   3,
		"Inbox":                4,
	}

	// New JSON format decodes exactly, without consulting known tags.
	tags, err := parseTagHistoryValue(`["a","b"]`, known)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, tags)

	// Legacy Go %v format from before the fix.
	tags, err = parseTagHistoryValue(`[paperless-gpt-auto paperless-gpt-failed]`, known)
	require.NoError(t, err)
	assert.Equal(t, []string{"paperless-gpt-auto", "paperless-gpt-failed"}, tags)

	// Empty.
	tags, err = parseTagHistoryValue(`[]`, known)
	require.NoError(t, err)
	assert.Empty(t, tags)

	tags, err = parseTagHistoryValue(`null`, known)
	require.NoError(t, err)
	assert.Empty(t, tags)
}

// Undo replaces a document's tags with the parsed list, so a legacy row whose
// tag names contain spaces must not be split on whitespace: "[Inbox Boîte de
// réception]" is two tags, not four. Splitting naively would remove the real
// tag on undo — and with CREATE_NEW_TAGS, create "Boîte", "de" and
// "réception" as new tags.
func TestParseTagHistoryValue_LegacyMultiWordTags(t *testing.T) {
	known := map[string]int{"Inbox": 4, "Boîte de réception": 3, "Boîte": 9}

	tags, err := parseTagHistoryValue(`[Inbox Boîte de réception]`, known)
	require.NoError(t, err)
	assert.Equal(t, []string{"Inbox", "Boîte de réception"}, tags,
		"longest existing name wins, even when a shorter prefix is also a tag")

	// Case differences resolve to the stored spelling.
	tags, err = parseTagHistoryValue(`[inbox]`, known)
	require.NoError(t, err)
	assert.Equal(t, []string{"Inbox"}, tags)
}

// When a legacy row cannot be mapped back onto existing tags, undo must refuse
// rather than guess: the tag may have been renamed or deleted since, or the
// row may hold IDs (old "New" values looked like "[3 2]").
func TestParseTagHistoryValue_LegacyUnresolvableIsRefused(t *testing.T) {
	known := map[string]int{"Inbox": 4}

	for _, raw := range []string{`[Inbox Deleted Tag]`, `[3 2]`} {
		_, err := parseTagHistoryValue(raw, known)
		assert.ErrorIs(t, err, errAmbiguousLegacyTags, raw)
	}
}

func TestMarshalTagsForHistory_EmptyIsJSONArray(t *testing.T) {
	assert.Equal(t, `[]`, marshalTagsForHistory(nil))
	assert.Equal(t, `[]`, marshalTagsForHistory([]string{}))
	var decoded []string
	require.NoError(t, json.Unmarshal([]byte(marshalTagsForHistory([]string{"a"})), &decoded))
	assert.Equal(t, []string{"a"}, decoded)
}
