package main

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProcessJobClearsPDFPasswordOnValidationFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldStore := jobStore
	store := &JobStore{jobs: map[string]*Job{}}
	jobStore = store
	t.Cleanup(func() { jobStore = oldStore })

	job := &Job{
		ID:         "password-cleanup-validation",
		DocumentID: 1,
		Options: OCROptions{
			PDFPassword:     "transient-secret",
			ReplaceOriginal: true,
			UploadPDF:       false,
		},
	}
	store.addJob(job)
	processJob(&App{Database: db}, job)

	retained, ok := store.getJob(job.ID)
	if !ok {
		t.Fatal("terminal job was not retained")
	}
	if retained.Status != "failed" {
		t.Fatalf("expected validation failure status, got %q", retained.Status)
	}
	if retained.Options.PDFPassword != "" {
		t.Fatalf("PDF password retained after processJob returned: %q", retained.Options.PDFPassword)
	}
}
