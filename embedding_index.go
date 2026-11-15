package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"paperless-gpt/sanitize"
)

const embeddingIndexPageSize = 100

var (
	errEmbeddingsDisabled  = errors.New("embedding provider is not configured")
	errEmbeddingJobRunning = errors.New("an embedding index job is already running")
)

// documentCatalog is the slice of the paperless-ngx API the index job needs.
type documentCatalog interface {
	DocumentCount(ctx context.Context) (int, error)
	ListDocumentStubs(ctx context.Context, page, pageSize int) ([]DocumentStub, int, error)
	GetDocument(ctx context.Context, documentID int) (Document, error)
}

// EmbeddingStatus is the progress payload for GET /api/embeddings.
type EmbeddingStatus struct {
	Enabled           bool   `json:"enabled"`
	Model             string `json:"model,omitempty"`
	State             string `json:"state"`
	Mode              string `json:"mode,omitempty"`
	Available         int    `json:"available"`
	Indexed           int    `json:"indexed"`
	Processed         int    `json:"processed"`
	Skipped           int    `json:"skipped"`
	Failed            int    `json:"failed"`
	Unchanged         int    `json:"unchanged"`
	CurrentDocumentID int    `json:"current_document_id"`
	Error             string `json:"error,omitempty"`
}

type embeddingJob struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	status  EmbeddingStatus
}

var embeddingJobInit sync.Mutex

func (app *App) ensureEmbeddingJob() *embeddingJob {
	embeddingJobInit.Lock()
	defer embeddingJobInit.Unlock()
	if app.embeddingJob == nil {
		app.embeddingJob = &embeddingJob{}
	}
	return app.embeddingJob
}

func (j *embeddingJob) snapshot() EmbeddingStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	status := j.status
	if status.State == "" {
		status.State = "idle"
	}
	return status
}

func (j *embeddingJob) update(fn func(*EmbeddingStatus)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.status)
}

func (app *App) embeddingStatus(ctx context.Context) EmbeddingStatus {
	status := app.ensureEmbeddingJob().snapshot()
	status.Enabled = app.Embedder != nil && app.Embeddings != nil
	if app.Embedder != nil {
		status.Model = app.Embedder.Model()
	}
	if app.Embeddings != nil {
		if n, err := app.Embeddings.Count(); err == nil {
			status.Indexed = n
		}
	}
	if !status.Enabled {
		status.State = "idle"
		return status
	}
	if status.State == "running" {
		return status
	}
	if catalog, ok := app.Client.(documentCatalog); ok {
		if n, err := catalog.DocumentCount(ctx); err == nil {
			status.Available = n
		} else {
			status.Error = err.Error()
		}
	}
	return status
}

func (app *App) startEmbeddingJob(mode string) error {
	if app.Embedder == nil || app.Embeddings == nil {
		return errEmbeddingsDisabled
	}
	catalog, ok := app.Client.(documentCatalog)
	if !ok {
		return fmt.Errorf("paperless client cannot list documents")
	}
	job := app.ensureEmbeddingJob()
	job.mu.Lock()
	if job.running {
		job.mu.Unlock()
		return errEmbeddingJobRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.running = true
	job.cancel = cancel
	job.status = EmbeddingStatus{
		Enabled: true,
		Model:   app.Embedder.Model(),
		State:   "running",
		Mode:    mode,
	}
	job.mu.Unlock()
	go app.runEmbeddingJob(ctx, mode, catalog)
	return nil
}

func (app *App) cancelEmbeddingJob() {
	if app.embeddingJob == nil {
		return
	}
	app.embeddingJob.mu.Lock()
	cancel := app.embeddingJob.cancel
	app.embeddingJob.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (app *App) runEmbeddingJob(ctx context.Context, mode string, catalog documentCatalog) {
	log.Infof("Embedding index %s started", mode)
	jobErr := app.indexDocuments(ctx, mode, catalog)
	app.embeddingJob.mu.Lock()
	app.embeddingJob.running = false
	app.embeddingJob.cancel = nil
	app.embeddingJob.status.State = "idle"
	app.embeddingJob.status.CurrentDocumentID = 0
	if jobErr != nil && !errors.Is(jobErr, context.Canceled) {
		app.embeddingJob.status.Error = jobErr.Error()
		log.Errorf("Embedding index %s failed: %v", mode, jobErr)
	} else if errors.Is(jobErr, context.Canceled) {
		app.embeddingJob.status.Error = ""
		log.Infof("Embedding index %s cancelled", mode)
	} else {
		app.embeddingJob.status.Error = ""
		log.Infof("Embedding index %s finished", mode)
	}
	app.embeddingJob.mu.Unlock()
}

func (app *App) indexDocuments(ctx context.Context, mode string, catalog documentCatalog) error {
	if mode == "rebuild" {
		if err := app.Embeddings.DeleteModel(); err != nil {
			return fmt.Errorf("clearing the embedding index: %w", err)
		}
	}
	seen := map[int]struct{}{}
	scanned := 0
	total := 0
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		stubs, count, err := catalog.ListDocumentStubs(ctx, page, embeddingIndexPageSize)
		if err != nil {
			return fmt.Errorf("listing documents: %w", err)
		}
		if page == 1 {
			total = count
			app.embeddingJob.update(func(status *EmbeddingStatus) { status.Available = count })
		}
		if len(stubs) == 0 {
			break
		}
		for _, stub := range stubs {
			if err := ctx.Err(); err != nil {
				return err
			}
			seen[stub.ID] = struct{}{}
			app.embeddingJob.update(func(status *EmbeddingStatus) { status.CurrentDocumentID = stub.ID })
			if err := app.indexOneDocument(ctx, mode, catalog, stub); err != nil {
				return err
			}
		}
		scanned += len(stubs)
		if total > 0 && scanned >= total {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := app.Embeddings.DeleteMissing(seen); err != nil {
		return fmt.Errorf("removing embeddings for deleted documents: %w", err)
	}
	return nil
}

// indexOneDocument records progress itself. A per-document failure increments
// the failed count and continues. A cancelled context stops the scan.
func (app *App) indexOneDocument(ctx context.Context, mode string, catalog documentCatalog, stub DocumentStub) error {
	if mode == "build" {
		if row, err := app.Embeddings.Get(stub.ID); err == nil && stub.Modified != "" && row.Modified == stub.Modified && row.Dimensions > 0 {
			app.embeddingJob.update(func(status *EmbeddingStatus) { status.Unchanged++ })
			return nil
		} else if err != nil && !errorsIsNotFound(err) {
			app.embeddingJob.update(func(status *EmbeddingStatus) {
				status.Failed++
				status.Error = err.Error()
			})
			return nil
		}
	}

	doc, err := catalog.GetDocument(ctx, stub.ID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		app.embeddingJob.update(func(status *EmbeddingStatus) {
			status.Failed++
			status.Error = err.Error()
		})
		log.WithField("document_id", stub.ID).Warnf("Skipping document while indexing embeddings: %v", err)
		return nil
	}
	if doc.Modified == "" {
		doc.Modified = stub.Modified
	}
	doc.Content = sanitize.Sanitize(doc.Content)
	prefix := embeddingPrefix(doc.Content)
	if prefix == "" {
		_ = app.Embeddings.Delete(doc.ID)
		app.embeddingJob.update(func(status *EmbeddingStatus) { status.Skipped++ })
		return nil
	}
	if mode == "build" {
		hash := hashEmbeddingText(prefix)
		if row, err := app.Embeddings.Get(doc.ID); err == nil && row.ContentHash == hash && row.Dimensions > 0 {
			if err := app.Embeddings.UpdateMetadata(doc); err != nil && !errorsIsNotFound(err) {
				log.WithField("document_id", doc.ID).Warnf("Could not refresh embedding metadata: %v", err)
			}
			app.embeddingJob.update(func(status *EmbeddingStatus) { status.Unchanged++ })
			return nil
		}
	}

	vectors, err := app.Embedder.Embed(ctx, []string{prefix})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		app.embeddingJob.update(func(status *EmbeddingStatus) {
			status.Failed++
			status.Error = err.Error()
		})
		log.WithField("document_id", doc.ID).Warnf("Embedding failed: %v", err)
		return nil
	}
	if len(vectors) != 1 {
		app.embeddingJob.update(func(status *EmbeddingStatus) {
			status.Failed++
			status.Error = "embedding model returned no vector"
		})
		return nil
	}
	row, err := embeddingRowFromDocument(doc, vectors[0])
	if err != nil {
		app.embeddingJob.update(func(status *EmbeddingStatus) { status.Skipped++ })
		return nil
	}
	if err := app.Embeddings.Upsert(row); err != nil {
		app.embeddingJob.update(func(status *EmbeddingStatus) {
			status.Failed++
			status.Error = err.Error()
		})
		return nil
	}
	app.embeddingJob.update(func(status *EmbeddingStatus) { status.Processed++ })
	return nil
}
