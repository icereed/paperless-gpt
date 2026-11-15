package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLMutationResponseIsUnconfirmed(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	env.setMockResponse("/api/documents/654/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>unexpected response</html>"))
	})
	response, err := env.client.Do(context.Background(), http.MethodPatch, "api/documents/654/", bytes.NewBufferString(`{}`))
	require.Error(t, err, "HTML response must not be accepted as API success")
	if response != nil {
		response.Body.Close()
	}
	data, err := os.ReadFile(os.Getenv("OCR_AUDIT_PATH"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var outcome mutationAuditEvent
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &outcome))
	assert.Equal(t, "unconfirmed", outcome.Outcome)
}

func TestTornAuditTailBlocksMutation(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	path := os.Getenv("OCR_AUDIT_PATH")
	partial := []byte(`{"event_id":"interrupted`)
	require.NoError(t, os.WriteFile(path, partial, 0o600))
	env.setMockResponse("/api/documents/654/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	response, err := env.client.Do(context.Background(), http.MethodPatch, "api/documents/654/", bytes.NewBufferString(`{}`))
	if response != nil {
		response.Body.Close()
	}
	require.Error(t, err)
	assert.Zero(t, env.requestCount, "no mutation may be dispatched after a partial audit record")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, partial, data, "do not join a fresh intent onto a torn record")
}

func TestNewDocumentUploadRequiresConfirmedImport(t *testing.T) {
	shortenTaskPolling(t)
	for _, replace := range []bool{false, true} {
		for _, status := range []string{"PENDING", "FAILURE", "SUCCESS"} {
			t.Run(fmt.Sprintf("replace=%v/%s", replace, status), func(t *testing.T) {
				env := newTestEnv(t)
				defer env.teardown()
				deleted := false
				env.setMockResponse("/api/documents/654/", func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodDelete {
						deleted = true
						w.WriteHeader(http.StatusNoContent)
						return
					}
					require.NoError(t, json.NewEncoder(w).Encode(GetDocumentApiResponse{ID: 654, Title: "Scan"}))
				})
				env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"count":0,"results":[]}`)) })
				env.setMockResponse("/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`"task-reviewed"`)) })
				env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
					_, _ = fmt.Fprintf(w, `{"count":1,"results":[{"task_id":"task-reviewed","status":%q}]}`, status)
				})
				app := &App{Client: env.client, Database: env.db}
				options := OCROptions{UploadPDF: true, ReplaceOriginal: replace}
				logger := logrus.WithField("test", "new-document-confirmation")
				uploadErr := app.uploadProcessedPDF(context.Background(), 654, []byte("%PDF-1.7"), options, logger)
				processed := &ProcessedDocument{ID: 654}
				setPDFUploadOutcome(processed, options, uploadErr, logger)
				if status == "SUCCESS" {
					require.NoError(t, uploadErr)
					assert.NoError(t, requiredPDFUploadError(options, processed))
				} else {
					require.Error(t, uploadErr)
					assert.Equal(t, "failed", processed.PDFAction)
					assert.Error(t, requiredPDFUploadError(options, processed), "unconfirmed/rejected import must not release automatic completion")
				}
				assert.Equal(t, replace && status == "SUCCESS", deleted)
			})
		}
	}
}
