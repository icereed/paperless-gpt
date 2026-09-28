package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTaskStatusReadsEveryResponseShape(t *testing.T) {
	cases := map[string]string{
		"bare object (older paperless-ngx)": `{"task_id":"t1","status":"SUCCESS"}`,
		"list":                              `[{"task_id":"t1","status":"SUCCESS"}]`,
		"paginated (paperless-ngx 3.0)":     `{"count":1,"next":null,"previous":null,"results":[{"task_id":"t1","status":"success"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			defer env.teardown()
			env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(body))
			})

			task, err := env.client.GetTaskStatus(context.Background(), "t1")
			require.NoError(t, err)
			assert.Equal(t, "t1", task["task_id"])
			assert.NotEmpty(t, task["status"])
		})
	}
}

func TestGetTaskStatusUnknownTask(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":0,"next":null,"previous":null,"results":[]}`))
	})

	_, err := env.client.GetTaskStatus(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// On paperless-ngx 3.0 the replace path used to find no top-level "status" in
// the paginated response and delete the original before the replacement was
// imported, even when that import failed.
func TestReplaceKeepsOriginalWhenPaperlessRejectsUpload(t *testing.T) {
	env := newTestEnv(t)
	defer env.teardown()

	documentID := 321
	deleted := false
	env.setMockResponse(fmt.Sprintf("/api/documents/%d/", documentID), func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(GetDocumentApiResponse{ID: documentID, Title: "Scan"})
	})
	env.setMockResponse("/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":0,"next":null,"previous":null,"results":[]}`))
	})
	env.setMockResponse("/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`"task-rep"`))
	})
	env.setMockResponse("/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":1,"results":[{"task_id":"task-rep","status":"failure"}]}`))
	})

	app := &App{Client: env.client, Database: env.db}
	options := OCROptions{UploadPDF: true, ReplaceOriginal: true}
	err := app.uploadProcessedPDF(context.Background(), documentID, []byte("%PDF-1.7"), options, logrus.WithField("test", "replace"))
	require.Error(t, err)
	assert.False(t, deleted, "the original must survive a rejected replacement")
}
