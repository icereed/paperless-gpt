package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogEnv is a test env whose tag, correspondent and document-type
// endpoints count full-catalog GETs per path.
func catalogEnv(t *testing.T, ttl time.Duration, tags *string) (*testEnv, map[string]int) {
	env := newTestEnv(t)
	env.client.CatalogCacheTTL = ttl
	calls := map[string]int{}
	countAndServe := func(body func() string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Query().Get("page_size") != "" {
				calls[r.URL.Path]++
			}
			if r.URL.Query().Get("name__iexact") != "" {
				_, _ = w.Write([]byte(`{"count":1,"results":[{"id":1,"document_count":1}]}`))
				return
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":99}`))
				return
			}
			_, _ = w.Write([]byte(body()))
		}
	}
	env.setMockResponse("/api/tags/", countAndServe(func() string { return *tags }))
	env.setMockResponse("/api/correspondents/", countAndServe(func() string { return `{"results":[{"id":1,"name":"Alpha"}]}` }))
	env.setMockResponse("/api/document_types/", countAndServe(func() string { return `{"results":[{"id":1,"name":"Invoice"}]}` }))
	return env, calls
}

// TestCatalogCacheRequestsPerBatch measures the catalog GETs of one
// background batch (list by tag, fetch each document, update the batch) with
// the cache disabled and enabled. Run with -v to print the numbers.
func TestCatalogCacheRequestsPerBatch(t *testing.T) {
	const docs = 10
	tags := `{"results":[{"id":1,"name":"paperless-gpt-auto"},{"id":2,"name":"done"}],"next":null}`

	run := func(ttl time.Duration) (catalog, total int) {
		env, calls := catalogEnv(t, ttl, &tags)
		defer env.teardown()

		var results []string
		for id := 1; id <= docs; id++ {
			results = append(results, fmt.Sprintf(`{"id":%d,"title":"t","tags":[1],"correspondent":1,"document_type":1}`, id))
			path := fmt.Sprintf("/api/documents/%d/", id)
			body := fmt.Sprintf(`{"id":%d,"title":"t","tags":[1],"correspondent":1,"document_type":1,"content":"c"}`, id)
			env.setMockResponse(path, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			})
		}
		env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"results":[` + strings.Join(results, ",") + `]}`))
		})

		ctx := context.Background()
		listed, err := env.client.GetDocumentsByTag(ctx, "paperless-gpt-auto", docs)
		require.NoError(t, err)
		var suggestions []DocumentSuggestion
		for _, d := range listed {
			doc, err := env.client.GetDocument(ctx, d.ID)
			require.NoError(t, err)
			suggestions = append(suggestions, DocumentSuggestion{
				ID: doc.ID, OriginalDocument: doc, SuggestedTitle: "new",
				SuggestedTags: []string{"done"}, RemoveTags: []string{"paperless-gpt-auto"},
			})
		}
		require.NoError(t, env.client.UpdateDocuments(ctx, suggestions, env.db, false))
		for _, n := range calls {
			catalog += n
		}
		return catalog, env.requestCount
	}

	uncached, uncachedTotal := run(0)
	cached, cachedTotal := run(30 * time.Second)
	t.Logf("%d-document batch: catalog GETs off=%d on=%d; all Paperless requests off=%d on=%d",
		docs, uncached, cached, uncachedTotal, cachedTotal)
	assert.Equal(t, 2+3*docs+3, uncached, "list (tags+correspondents) + 3 per GetDocument + 3 per UpdateDocuments")
	assert.Equal(t, 3, cached, "one GET per catalog")
}

func TestCatalogCacheInvalidation(t *testing.T) {
	tags := `{"results":[{"id":1,"name":"a"}],"next":null}`
	env, calls := catalogEnv(t, time.Minute, &tags)
	defer env.teardown()
	ctx := context.Background()

	_, err := env.client.GetAllTags(ctx)
	require.NoError(t, err)
	_, err = env.client.GetAllTags(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, calls["/api/tags/"], "second read is served from the cache")

	// A tag created outside paperless-gpt: a document referencing its ID is
	// a cache miss, not an invisible tag.
	tags = `{"results":[{"id":1,"name":"a"},{"id":2,"name":"b"}],"next":null}`
	env.setMockResponse("/api/documents/7/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"title":"t","tags":[2]}`))
	})
	doc, err := env.client.GetDocument(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, doc.Tags)
	assert.Equal(t, 2, calls["/api/tags/"])

	// Own writes invalidate.
	_, err = env.client.CreateTag(ctx, "c")
	require.NoError(t, err)
	_, err = env.client.GetAllTags(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, calls["/api/tags/"], "CreateTag invalidates the tag catalog")

	_, err = env.client.GetAllCorrespondents(ctx)
	require.NoError(t, err)
	_, err = env.client.CreateOrGetCorrespondent(ctx, Correspondent{Name: "Alpha"})
	require.NoError(t, err)
	assert.Equal(t, 2, calls["/api/correspondents/"], "create-or-get looks up against a fresh catalog")

	// A rejected update (e.g. a deleted document type still cached) drops all catalogs.
	_, err = env.client.GetAllDocumentTypes(ctx)
	require.NoError(t, err)
	env.setMockResponse("/api/documents/8/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"document_type":["Invalid pk \"1\" - object does not exist."]}`))
	})
	err = env.client.UpdateDocuments(ctx, []DocumentSuggestion{{ID: 8, OriginalDocument: Document{ID: 8}, SuggestedTitle: "x", SuggestedDocumentType: "Invoice"}}, env.db, false)
	require.Error(t, err)
	_, err = env.client.GetAllDocumentTypes(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, calls["/api/document_types/"], "refetched after the rejected PATCH")
}

func TestCatalogCacheUnresolvableTagRefetchedOnce(t *testing.T) {
	tags := `{"results":[{"id":1,"name":"a"}],"next":null}`
	env, calls := catalogEnv(t, time.Minute, &tags)
	defer env.teardown()
	ctx := context.Background()

	// Tag 5 exists but is not visible to the API user, so no refetch resolves it.
	env.setMockResponse("/api/documents/7/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"title":"t","tags":[1,5]}`))
	})
	_, err := env.client.GetAllTags(ctx)
	require.NoError(t, err)
	for range 3 {
		doc, err := env.client.GetDocument(ctx, 7)
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, doc.Tags)
	}
	assert.Equal(t, 2, calls["/api/tags/"], "one refetch for the unknown ID, then served from the cache")
}

func TestCatalogCacheDisabledDoesNotSerializeFetches(t *testing.T) {
	var c catalogCache[int]
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _, _ = c.get(0, func() (int, error) {
			close(started)
			<-release
			return 1, nil
		})
	}()
	<-started
	done := make(chan struct{})
	go func() {
		_, _, _ = c.get(0, func() (int, error) { return 2, nil })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a disabled cache serialized concurrent fetches")
	}
	close(release)
}
