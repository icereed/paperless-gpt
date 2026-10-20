package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownloadDocumentAsPDF_RecoversPdfcpuPanic verifies that a panic inside
// pdfcpu (#1203) is returned as an error instead of crashing the process.
func TestDownloadDocumentAsPDF_RecoversPdfcpuPanic(t *testing.T) {
	pdfContent, err := os.ReadFile("tests/pdf/five-pager.pdf")
	require.NoError(t, err)

	panicky := func(string, string, []string, *model.Configuration) error {
		panic("assignment to entry in nil map")
	}

	tests := []struct {
		name       string
		limitPages int
		setup      func()
		wantOp     string
	}{
		{
			name:       "split",
			limitPages: 0,
			setup: func() {
				pdfSplitFile = func(string, string, int, *model.Configuration) error {
					panic("assignment to entry in nil map")
				}
			},
			wantOp: "split",
		},
		{
			name:       "trim",
			limitPages: 2,
			setup:      func() { pdfTrimFile = panicky },
			wantOp:     "trim",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origTrim, origSplit := pdfTrimFile, pdfSplitFile
			t.Cleanup(func() { pdfTrimFile, pdfSplitFile = origTrim, origSplit })
			tt.setup()

			env := newTestEnv(t)
			defer env.teardown()

			documentID := 1203 + i
			env.setMockResponse(fmt.Sprintf("/api/documents/%d/download/", documentID), func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(pdfContent)
			})
			env.client.CacheFolder = t.TempDir()

			_, _, _, err := env.client.DownloadDocumentAsPDF(context.Background(), documentID, tt.limitPages, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "pdfcpu "+tt.wantOp+" panicked")
			assert.Contains(t, err.Error(), "assignment to entry in nil map")
		})
	}
}
