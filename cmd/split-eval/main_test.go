package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBoundaryReport checks boundary classifications, aggregate counts, and zero-denominator ratio formatting.
func TestBoundaryReport(t *testing.T) {
	var out bytes.Buffer
	tp, fp, fn := reportBoundaries(&out, 6, []int{2, 4}, []int{2, 3})
	assert.Equal(t, [3]int{1, 1, 1}, [3]int{tp, fp, fn})
	assert.Contains(t, out.String(), "2\ttrue\ttrue\tTP")
	assert.Contains(t, out.String(), "3\tfalse\ttrue\tFP")
	assert.Contains(t, out.String(), "4\ttrue\tfalse\tFN")
	assert.Equal(t, "n/a", ratio(0, 0))
	assert.Equal(t, "0.5000", ratio(1, 2))
}

// TestEvaluateDryRun checks batch evaluation, token isolation, and rejection before upload when dry-run mode is disabled.
func TestEvaluateDryRun(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		t.Run(fmt.Sprint(dryRun), func(t *testing.T) {
			folder := t.TempDir()
			pdf, err := os.ReadFile("../../tests/pdf/sample.pdf")
			require.NoError(t, err)
			pages, err := api.PageCount(bytes.NewReader(pdf), nil)
			require.NoError(t, err)
			for _, name := range []string{"a.pdf", "b.PDF"} {
				require.NoError(t, os.WriteFile(filepath.Join(folder, name), pdf, 0600))
			}
			uploads, generations := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/split-suggestions":
					fmt.Fprintf(w, `{"config":{"dry_run":%t}}`, dryRun)
				case "/api/documents/post_document/":
					uploads++
					assert.Equal(t, "Token test-token", r.Header.Get("Authorization"))
					require.NoError(t, r.ParseMultipartForm(32<<20))
					defer r.MultipartForm.RemoveAll()
					file, _, err := r.FormFile("document")
					require.NoError(t, err)
					defer file.Close()
					count, err := api.PageCount(file, nil)
					require.NoError(t, err)
					assert.Equal(t, 2*pages, count)
					assert.Empty(t, r.FormValue("tags"))
					fmt.Fprint(w, `"task-id"`)
				case "/api/tasks/":
					assert.Equal(t, "task-id", r.URL.Query().Get("task_id"))
					fmt.Fprint(w, `[{"status":"SUCCESS","related_document_ids":[17]}]`)
				case "/api/documents/17/split-suggestions":
					generations++
					assert.Empty(t, r.Header.Get("Authorization"), "Paperless token must not be forwarded to GPT")
					fmt.Fprintf(w, `{"status":"review","page_count":%d,"boundaries":[{"after_page":%d}]}`, 2*pages, pages)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			var out bytes.Buffer
			err = evaluate(context.Background(), options{input: folder, paperlessURL: server.URL, gptURL: server.URL, token: "test-token", batchSize: 3}, &out)
			if dryRun {
				require.NoError(t, err)
				assert.Equal(t, 1, uploads)
				assert.Equal(t, 1, generations)
				assert.Contains(t, out.String(), "precision=1.0000 recall=1.0000")
			} else {
				require.ErrorContains(t, err, "SPLIT_DRY_RUN=true")
				assert.Zero(t, uploads)
				assert.Zero(t, generations)
			}
		})
	}
}

// TestTaskAndSuggestionValidation checks task result shapes, failure handling, URL restrictions, and undefined ratios.
func TestTaskAndSuggestionValidation(t *testing.T) {
	for _, body := range []string{`[]`, `{"results":[{"status":"STARTED"}]}`} {
		id, err := taskDocumentID([]byte(body))
		require.NoError(t, err)
		assert.Zero(t, id)
	}
	id, err := taskDocumentID([]byte(`{"results":[{"status":"SUCCESS","result_data":{"document_id":23}}]}`))
	require.NoError(t, err)
	assert.Equal(t, 23, id)
	_, err = taskDocumentID([]byte(`[{"status":"FAILURE"}]`))
	require.Error(t, err)
	_, err = taskDocumentID([]byte(`{"wrong":"shape"}`))
	require.Error(t, err)
	for _, raw := range []string{"", "ftp://host", "http://user:secret@host"} {
		require.Error(t, validateURL(raw))
	}
	require.NoError(t, validateURL("http://localhost:8080/prefix"))
	assert.False(t, strings.Contains(ratio(0, 0), "NaN"))
}
