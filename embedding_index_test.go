package main

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCatalog struct {
	stubs []DocumentStub
	docs  map[int]Document
}

func (f fakeCatalog) DocumentCount(context.Context) (int, error) {
	return len(f.stubs), nil
}

func (f fakeCatalog) ListDocumentStubs(_ context.Context, page, pageSize int) ([]DocumentStub, int, error) {
	if page < 1 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(f.stubs) {
		return nil, len(f.stubs), nil
	}
	end := start + pageSize
	if end > len(f.stubs) {
		end = len(f.stubs)
	}
	return f.stubs[start:end], len(f.stubs), nil
}

func (f fakeCatalog) GetDocument(_ context.Context, id int) (Document, error) {
	doc, ok := f.docs[id]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	return doc, nil
}

type staticEmbedder struct {
	mu    sync.Mutex
	calls int
}

func (e *staticEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	return [][]float32{{1, 0, 0}}, nil
}

func (e *staticEmbedder) Model() string { return "nomic" }

func (e *staticEmbedder) Calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestIndexBuildSkipsUnchangedAndRebuilds(t *testing.T) {
	catalog := fakeCatalog{
		stubs: []DocumentStub{
			{ID: 1, Modified: "2024-01-01T00:00:00Z"},
			{ID: 2, Modified: "2024-01-02T00:00:00Z"},
		},
		docs: map[int]Document{
			1: {ID: 1, Title: "Bill", Content: "amount due", Tags: []string{"utilities"}, Correspondent: "Stadtwerke", DocumentTypeName: "Invoice", Modified: "2024-01-01T00:00:00Z"},
			2: {ID: 2, Title: "Empty", Content: "", Modified: "2024-01-02T00:00:00Z"},
		},
	}
	embedder := &staticEmbedder{}
	app := &App{
		Embedder:     embedder,
		Embeddings:   NewEmbeddingStore(openEmbeddingDB(t), "nomic"),
		embeddingJob: &embeddingJob{},
	}
	require.NoError(t, app.indexDocuments(context.Background(), "build", catalog))
	assert.Equal(t, 1, embedder.Calls())
	count, err := app.Embeddings.Count()
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	status := app.embeddingJob.snapshot()
	assert.Equal(t, 1, status.Processed)
	assert.Equal(t, 1, status.Skipped)

	require.NoError(t, app.indexDocuments(context.Background(), "build", catalog))
	assert.Equal(t, 1, embedder.Calls(), "unchanged documents are not embedded again")

	require.NoError(t, app.indexDocuments(context.Background(), "rebuild", catalog))
	assert.Equal(t, 2, embedder.Calls())
}

func TestIndexBuildDropsDeletedDocuments(t *testing.T) {
	db := openEmbeddingDB(t)
	store := NewEmbeddingStore(db, "nomic")
	require.NoError(t, store.Upsert(mustRow(t, Document{ID: 9, Content: "gone"}, []float32{0, 1, 0})))
	app := &App{
		Embedder:     &staticEmbedder{},
		Embeddings:   store,
		embeddingJob: &embeddingJob{},
	}
	catalog := fakeCatalog{
		stubs: []DocumentStub{{ID: 1, Modified: "2024-01-01T00:00:00Z"}},
		docs:  map[int]Document{1: {ID: 1, Content: "kept", Modified: "2024-01-01T00:00:00Z"}},
	}
	require.NoError(t, app.indexDocuments(context.Background(), "build", catalog))
	_, err := store.Get(9)
	assert.Error(t, err)
	_, err = store.Get(1)
	assert.NoError(t, err)
}

type cancelEmbedder struct {
	cancel context.CancelFunc
	calls  int
}

func (e *cancelEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	e.calls++
	if e.calls == 1 {
		e.cancel()
	}
	return [][]float32{{1, 0}}, nil
}

func (e *cancelEmbedder) Model() string { return "nomic" }

func TestIndexCancelStopsBetweenDocuments(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	embedder := &cancelEmbedder{cancel: cancel}
	app := &App{
		Embedder:     embedder,
		Embeddings:   NewEmbeddingStore(openEmbeddingDB(t), "nomic"),
		embeddingJob: &embeddingJob{},
	}
	catalog := fakeCatalog{
		stubs: []DocumentStub{{ID: 1, Modified: "a"}, {ID: 2, Modified: "b"}},
		docs: map[int]Document{
			1: {ID: 1, Content: "one", Modified: "a"},
			2: {ID: 2, Content: "two", Modified: "b"},
		},
	}
	err := app.indexDocuments(ctx, "build", catalog)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, embedder.calls)
	count, countErr := app.Embeddings.Count()
	require.NoError(t, countErr)
	assert.Equal(t, 1, count)
}

func TestDocumentCountAndStubs(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	env.setMockResponse("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("fields") {
		case "id":
			_, _ = w.Write([]byte(`{"count":2,"results":[{"id":1}]}`))
		case "id,modified":
			assert.Equal(t, "id", r.URL.Query().Get("ordering"))
			assert.Equal(t, "1", r.URL.Query().Get("page"))
			_, _ = w.Write([]byte(`{"count":2,"results":[{"id":1,"modified":"2024-01-01T00:00:00Z"},{"id":2,"modified":"2024-02-01T00:00:00Z"}]}`))
		default:
			t.Fatalf("unexpected fields %q", r.URL.Query().Get("fields"))
		}
	})

	count, err := env.client.DocumentCount(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	stubs, total, err := env.client.ListDocumentStubs(context.Background(), 1, 100)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, stubs, 2)
	assert.Equal(t, 1, stubs[0].ID)
	assert.Equal(t, "2024-02-01T00:00:00Z", stubs[1].Modified)
}

func TestStartEmbeddingJobRejectsASecondRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	app := &App{
		Client: catalogClient{
			mockPaperlessClient: &mockPaperlessClient{},
			catalog: fakeCatalog{
				stubs: []DocumentStub{{ID: 1, Modified: "a"}},
				docs:  map[int]Document{1: {ID: 1, Content: "text", Modified: "a"}},
			},
		},
		Embedder:   &blockingEmbedder{started: started, release: release},
		Embeddings: NewEmbeddingStore(openEmbeddingDB(t), "nomic"),
	}
	require.NoError(t, app.startEmbeddingJob("build"))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("index job did not start")
	}
	err := app.startEmbeddingJob("rebuild")
	assert.ErrorIs(t, err, errEmbeddingJobRunning)
	close(release)
	require.Eventually(t, func() bool {
		return app.embeddingJob.snapshot().State == "idle"
	}, 2*time.Second, 10*time.Millisecond)
}

// catalogClient is a paperless client whose document list comes from a fake catalog.
type catalogClient struct {
	*mockPaperlessClient
	catalog fakeCatalog
}

func (c catalogClient) DocumentCount(ctx context.Context) (int, error) {
	return c.catalog.DocumentCount(ctx)
}

func (c catalogClient) ListDocumentStubs(ctx context.Context, page, pageSize int) ([]DocumentStub, int, error) {
	return c.catalog.ListDocumentStubs(ctx, page, pageSize)
}

func (c catalogClient) GetDocument(ctx context.Context, id int) (Document, error) {
	return c.catalog.GetDocument(ctx, id)
}

type blockingEmbedder struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingEmbedder) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return [][]float32{{1, 0}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingEmbedder) Model() string { return "nomic" }
